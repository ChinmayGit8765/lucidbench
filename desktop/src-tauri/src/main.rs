// Hide the console window in release builds on Windows.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

#[cfg(windows)]
mod win;
mod engine;

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
    probe(port, timeout).is_some()
}

/// `None` when nothing healthy answers; otherwise the version the engine reports.
fn probe(port: u16, timeout: Duration) -> Option<Option<String>> {
    let sock = ("127.0.0.1", port).to_socket_addrs().ok()?.next()?;
    health_at(sock, timeout)
}

fn health_at(sock: SocketAddr, timeout: Duration) -> Option<Option<String>> {
    let mut s = TcpStream::connect_timeout(&sock, timeout).ok()?;
    let _ = s.set_read_timeout(Some(timeout));
    let _ = s.set_write_timeout(Some(timeout));
    let req = format!(
        "GET /api/health HTTP/1.1\r\nHost: 127.0.0.1:{}\r\nConnection: close\r\n\r\n",
        sock.port()
    );
    if s.write_all(req.as_bytes()).is_err() {
        return None;
    }
    // The response is tiny and the server closes the connection; read it all.
    let mut raw = Vec::new();
    let _ = (&mut s).take(16 * 1024).read_to_end(&mut raw);
    let text = String::from_utf8_lossy(&raw);
    if !text.starts_with("HTTP/") || !text.lines().next().unwrap_or("").contains(" 200") {
        return None;
    }
    Some(engine::parse_health_version(&text))
}

/// PID of the bundled sidecar when it is what serves `port` (Windows only).
#[cfg(windows)]
fn bundled_pid_on(port: u16) -> Option<u32> {
    let pid = win::listener_pid(port)?;
    let running = win::process_path(pid)?;
    let exe = std::env::current_exe().ok()?;
    let bundled = exe.parent()?.join("lucidd.exe");
    engine::same_path(&running, &bundled).then_some(pid)
}
#[cfg(not(windows))]
fn bundled_pid_on(_: u16) -> Option<u32> {
    None
}

#[cfg(windows)]
fn kill_pid(pid: u32) {
    win::kill_pid(pid);
}
#[cfg(not(windows))]
fn kill_pid(_: u32) {}

/// Banner injected into the engine's UI when its version differs from the app's.
fn notice_script(msg: &str) -> String {
    let m = serde_json::to_string(msg).unwrap_or_else(|_| "\"\"".into());
    format!(
        "(function(){{if(window.__lucidNotice)return;window.__lucidNotice=1;\
var d=document.createElement('div');\
d.style.cssText='position:fixed;right:16px;bottom:16px;z-index:2147483647;display:flex;gap:12px;align-items:center;padding:10px 14px;border-radius:8px;background:#2a2230;color:#f3e3a5;font:13px Segoe UI,system-ui,sans-serif;box-shadow:0 4px 18px rgba(0,0,0,.45)';\
var s=document.createElement('span');s.textContent={m};\
var b=document.createElement('button');b.textContent='\\u00d7';b.setAttribute('aria-label','Dismiss');\
b.style.cssText='background:none;border:0;color:inherit;font-size:18px;cursor:pointer;line-height:1';\
b.onclick=function(){{d.remove()}};d.append(s,b);document.body.appendChild(d)}})();"
    )
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

    let running = tauri::async_runtime::spawn_blocking(move || probe(port, ATTACH_TIMEOUT))
        .await
        .map_err(|e| e.to_string())?;

    let mut attach = running.is_some();
    let mut notice: Option<String> = None;
    if let Some(engine_version) = running {
        let app_version = app.package_info().version.to_string();
        let bundled = tauri::async_runtime::spawn_blocking(move || bundled_pid_on(port))
            .await
            .map_err(|e| e.to_string())?;
        match engine::decide(&app_version, engine_version.as_deref(), bundled.is_some()) {
            engine::Action::Attach => {}
            engine::Action::AttachWithNotice(msg) => notice = Some(msg),
            engine::Action::Restart => {
                status("Updating the Lucidbench engine...");
                let pid = bundled.unwrap_or_default();
                let gone = tauri::async_runtime::spawn_blocking(move || {
                    kill_pid(pid);
                    let deadline = Instant::now() + Duration::from_secs(5);
                    while Instant::now() < deadline {
                        if !healthy(port, Duration::from_millis(300)) {
                            return true;
                        }
                        std::thread::sleep(Duration::from_millis(200));
                    }
                    false
                })
                .await
                .map_err(|e| e.to_string())?;
                if gone {
                    attach = false;
                } else {
                    notice = Some(format!(
                        "Engine v{} differs from app v{}",
                        engine_version.as_deref().unwrap_or("unknown"),
                        app_version
                    ));
                }
            }
        }
    }

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
        .map_err(|e| e.to_string())?;

    if let Some(msg) = notice {
        // The page loads asynchronously; inject the banner once it is likely up.
        let script = notice_script(&msg);
        std::thread::spawn(move || {
            for delay in [1500u64, 2500] {
                std::thread::sleep(Duration::from_millis(delay));
                let _ = win.eval(&script);
            }
        });
    }
    Ok(())
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
