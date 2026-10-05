//! Pure decisions about an engine (daemon) that is already running when the app
//! starts. No I/O here, so it is unit-tested.

use std::path::Path;

#[derive(Debug, PartialEq, Eq)]
pub enum Action {
    /// Versions agree: use the running engine.
    Attach,
    /// Versions differ and the engine is this app's own bundled sidecar: replace it.
    Restart,
    /// Versions differ but the engine is not ours to stop (docker, a dev build):
    /// attach and tell the user.
    AttachWithNotice(String),
}

/// "v0.2.0" and "0.2.0" are the same version.
fn normalize(v: &str) -> &str {
    let v = v.trim();
    v.strip_prefix('v').or_else(|| v.strip_prefix('V')).unwrap_or(v)
}

pub fn versions_match(app: &str, engine: &str) -> bool {
    normalize(app) == normalize(engine)
}

/// `engine` is the version the running engine reports (None when it did not say).
/// `owned_by_bundled` is true when the process serving the port is this app's
/// bundled sidecar executable.
pub fn decide(app: &str, engine: Option<&str>, owned_by_bundled: bool) -> Action {
    match engine {
        Some(e) if versions_match(app, e) => Action::Attach,
        _ if owned_by_bundled => Action::Restart,
        other => Action::AttachWithNotice(format!(
            "Engine v{} differs from app v{}",
            other.map(normalize).unwrap_or("unknown"),
            normalize(app)
        )),
    }
}

/// Windows paths compare case-insensitively and may carry a `\\?\` prefix.
pub fn same_path(a: &Path, b: &Path) -> bool {
    fn key(p: &Path) -> String {
        let s = p.to_string_lossy().replace('/', "\\");
        s.strip_prefix(r"\\?\").unwrap_or(&s).to_lowercase()
    }
    key(a) == key(b)
}

/// Pull `version` out of a `/api/health` HTTP response (headers and body).
pub fn parse_health_version(response: &str) -> Option<String> {
    let body = response.split_once("\r\n\r\n").map(|(_, b)| b)?;
    let start = body.find('{')?;
    let end = body.rfind('}')?;
    let v: serde_json::Value = serde_json::from_str(&body[start..=end]).ok()?;
    v.get("version")?.as_str().map(str::to_string)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    #[test]
    fn matching_versions_attach() {
        assert_eq!(decide("0.2.0", Some("0.2.0"), true), Action::Attach);
        assert_eq!(decide("0.2.0", Some("v0.2.0"), false), Action::Attach);
        assert_eq!(decide("v0.2.0", Some(" 0.2.0 "), false), Action::Attach);
    }

    #[test]
    fn stale_bundled_engine_is_restarted() {
        assert_eq!(decide("0.2.0", Some("0.1.0"), true), Action::Restart);
        assert_eq!(decide("0.2.0", None, true), Action::Restart);
    }

    #[test]
    fn foreign_engine_is_attached_with_a_notice() {
        assert_eq!(
            decide("0.2.0", Some("0.1.0"), false),
            Action::AttachWithNotice("Engine v0.1.0 differs from app v0.2.0".into())
        );
        assert_eq!(
            decide("0.2.0", Some("dev"), false),
            Action::AttachWithNotice("Engine vdev differs from app v0.2.0".into())
        );
        assert_eq!(
            decide("0.2.0", None, false),
            Action::AttachWithNotice("Engine vunknown differs from app v0.2.0".into())
        );
    }

    #[test]
    fn a_newer_engine_also_counts_as_a_mismatch() {
        assert_eq!(decide("0.2.0", Some("0.3.0"), true), Action::Restart);
    }

    #[test]
    fn paths_compare_case_insensitively() {
        let a = PathBuf::from(r"C:\Users\<you>\AppData\Local\Lucidbench\lucidd.exe");
        let b = PathBuf::from(r"\\?\c:\users\<you>\appdata\local\lucidbench\LUCIDD.EXE");
        assert!(same_path(&a, &b));
        assert!(!same_path(&a, &PathBuf::from(r"C:\other\lucidd.exe")));
    }

    #[test]
    fn health_version_is_parsed() {
        let ok = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"status\":\"ok\",\"version\":\"0.2.0\"}\n";
        assert_eq!(parse_health_version(ok).as_deref(), Some("0.2.0"));
        let chunked = "HTTP/1.1 200 OK\r\n\r\n26\r\n{\"status\":\"ok\",\"version\":\"v0.1.0\"}\n\r\n0\r\n\r\n";
        assert_eq!(parse_health_version(chunked).as_deref(), Some("v0.1.0"));
        assert_eq!(parse_health_version("HTTP/1.1 200 OK\r\n\r\n{\"status\":\"ok\"}"), None);
        assert_eq!(parse_health_version("garbage"), None);
    }
}
