// Hide the console window in release builds on Windows.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

#[cfg(windows)]
mod win;

use std::io::{Read, Write};
use std::net::{SocketAddr, TcpStream, ToSocketAddrs};
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use tauri::webview::Color;
use tauri::{AppHandle, Emitter, Manager, RunEvent, State, WebviewUrl, WebviewWindowBuilder};

const DEFAULT_ADDR: &str = "127.0.0.1:7420";
const ATTACH_TIMEOUT: Duration = Duration::from_millis(1500);
const SPAWN_TIMEOUT: Duration = Duration::from_secs(15);

/// State shared with the splash page commands.
struct Daemon {
    port: u16,
    /// Set only when this app spawned the sidecar; only then is it killed on exit.
    child: Mutex<Option<Child>>,
}

/// Port from `LUCID_SERVER_ADDR` / `LUCID_ADDR` (":7420", "0.0.0.0:7420", "host:port").
/// The desktop app always talks to loopback, whatever host the daemon binds.
fn daemon_addr() -> String {
    ["LUCID_SERVER_ADDR", "LUCID_ADDR"]
        .iter()
        .filter_map(|k| std::env::var(k).ok())
        .map(|v| v.trim().to_string())
        .find(|v| !v.is_empty())
        .unwrap_or_else(|| DEFAULT_ADDR.to_string())
}

fn parse_port(addr: &str) -> u16 {
    addr.rsplit(':')
        .next()
        .and_then(|p| p.parse().ok())
        .unwrap_or(7420)
}

/// True when `GET /api/health` answers 200 within `timeout`.
fn healthy(port: u16, timeout: Duration) -> bool {
    let Some(sock) = ("127.0.0.1", port)
        .to_socket_addrs()
        .ok()
        .and_then(|mut a| a.next())
    else {
        return false;
    };
    health_at(sock, timeout)
}

fn health_at(sock: SocketAddr, timeout: Duration) -> bool {
    let Ok(mut s) = TcpStream::connect_timeout(&sock, timeout) else {
        return false;
    };
    let _ = s.set_read_timeout(Some(timeout));
    let _ = s.set_write_timeout(Some(timeout));
    let req = format!(
        "GET /api/health HTTP/1.1\r\nHost: 127.0.0.1:{}\r\nConnection: close\r\n\r\n",
        sock.port()
    );
    if s.write_all(req.as_bytes()).is_err() {
        return false;
    }
    let mut buf = [0u8; 32];
    match s.read(&mut buf) {
        Ok(n) => String::from_utf8_lossy(&buf[..n]).contains(" 200"),
        Err(_) => false,
    }
}

#[cfg(windows)]
fn hide_console(cmd: &mut Command) {
    use std::os::windows::process::CommandExt;
    cmd.creation_flags(0x0800_0000); // CREATE_NO_WINDOW
}
#[cfg(not(windows))]
fn hide_console(_: &mut Command) {}

/// The bundled sidecar sits next to the app exe as `lucidd.exe` once installed.
fn spawn_sidecar(port: u16) -> Result<Child, String> {
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let dir = exe.parent().ok_or("cannot locate the app directory")?;
    let name = if cfg!(windows) { "lucidd.exe" } else { "lucidd" };
    let path = dir.join(name);
    if !path.exists() {
        return Err(format!("bundled daemon not found: {}", path.display()));
    }
    let mut cmd = Command::new(&path);
    cmd.env("LUCID_ADDR", format!("127.0.0.1:{port}"))
        .env_remove("LUCID_SERVER_ADDR")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    hide_console(&mut cmd);
    let child = cmd
        .spawn()
        .map_err(|e| format!("could not start the daemon: {e}"))?;
    // Tie the daemon to this app: it dies with us, even on a forced kill.
    #[cfg(windows)]
    win::bind_to_job(&child);
    Ok(child)
}

/// Attach to a running daemon, or spawn the bundled one, then show the UI.
#[tauri::command]
async fn boot(app: AppHandle, state: State<'_, Daemon>) -> Result<(), String> {
    let port = state.port;
    let status = |m: &str| {
        let _ = app.emit("boot-status", m.to_string());
    };

    let attach = tauri::async_runtime::spawn_blocking(move || healthy(port, ATTACH_TIMEOUT))
        .await
        .map_err(|e| e.to_string())?;

    if !attach {
        status("Starting the Lucidbench daemon...");
        let already = state.child.lock().unwrap().is_some();
        if !already {
            let child = spawn_sidecar(port)?;
            *state.child.lock().unwrap() = Some(child);
        }
        let ready = tauri::async_runtime::spawn_blocking(move || {
            let deadline = Instant::now() + SPAWN_TIMEOUT;
            while Instant::now() < deadline {
                if healthy(port, Duration::from_millis(500)) {
                    return true;
                }
                std::thread::sleep(Duration::from_millis(250));
            }
            false
        })
        .await
        .map_err(|e| e.to_string())?;
        if !ready {
            // Surface an early exit (for example the port being taken by something else).
            let exited = state
                .child
                .lock()
                .unwrap()
                .as_mut()
                .and_then(|c| c.try_wait().ok().flatten());
            return Err(match exited {
                Some(code) => format!("The daemon exited early ({code}). Is port {port} in use?"),
                None => format!("The daemon did not answer on port {port} within 15 seconds."),
            });
        }
    }

    let url = format!("http://127.0.0.1:{port}/");
    let win = app.get_webview_window("main").ok_or("main window missing")?;
    win.navigate(url.parse().map_err(|e| format!("{e}"))?)
        .map_err(|e| e.to_string())
}

/// Allow only the splash (bundled) and the local daemon.
fn allowed(url: &tauri::Url, port: u16) -> bool {
    match url.scheme() {
        "tauri" | "about" | "data" => true,
        "http" | "https" => match url.host_str() {
            Some("tauri.localhost") => true,
            Some("127.0.0.1") | Some("localhost") => {
                url.scheme() == "http" && url.port_or_known_default() == Some(port)
            }
            _ => false,
        },
        _ => false,
    }
}

fn main() {
    let port = parse_port(&daemon_addr());

    let app = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(w) = app.get_webview_window("main") {
                let _ = w.unminimize();
                let _ = w.show();
                let _ = w.set_focus();
            }
        }))
        .plugin(tauri_plugin_window_state::Builder::default().build())
        .manage(Daemon {
            port,
            child: Mutex::new(None),
        })
        .invoke_handler(tauri::generate_handler![boot])
        .setup(move |app| {
            WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
                .title("Lucidbench")
                .inner_size(1360.0, 860.0)
                .min_inner_size(960.0, 640.0)
                .decorations(true)
                .background_color(Color(11, 13, 18, 255))
                .on_navigation(move |url| allowed(url, port))
                .build()?;
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building the Lucidbench app");

    app.run(|handle, event| {
        if let RunEvent::Exit = event {
            // Only a daemon this app spawned is ours to stop.
            if let Some(state) = handle.try_state::<Daemon>() {
                if let Some(mut child) = state.child.lock().unwrap().take() {
                    let _ = child.kill();
                    let _ = child.wait();
                }
            }
        }
    });
}
