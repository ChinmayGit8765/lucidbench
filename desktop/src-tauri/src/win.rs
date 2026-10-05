//! Windows process plumbing: a kill-on-close job object for the sidecar, and
//! lookups of which process owns a loopback port.

use std::os::windows::io::AsRawHandle;
use std::path::PathBuf;
use std::process::Child;
use std::sync::OnceLock;

use windows_sys::Win32::Foundation::{CloseHandle, HANDLE};
use windows_sys::Win32::NetworkManagement::IpHelper::{
    GetExtendedTcpTable, MIB_TCPROW_OWNER_PID, TCP_TABLE_OWNER_PID_LISTENER,
};
use windows_sys::Win32::Networking::WinSock::AF_INET;
use windows_sys::Win32::System::JobObjects::{
    AssignProcessToJobObject, CreateJobObjectW, JobObjectExtendedLimitInformation,
    SetInformationJobObject, JOBOBJECT_EXTENDED_LIMIT_INFORMATION,
    JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
};
use windows_sys::Win32::System::Threading::{
    OpenProcess, QueryFullProcessImageNameW, PROCESS_QUERY_LIMITED_INFORMATION,
};

/// The job handle lives for the whole process. When the app dies, even by a
/// forced kill, the OS closes it and takes everything inside the job with it.
struct Job(HANDLE);
unsafe impl Send for Job {}
unsafe impl Sync for Job {}

static JOB: OnceLock<Option<Job>> = OnceLock::new();

fn job() -> Option<&'static Job> {
    JOB.get_or_init(|| unsafe {
        let h = CreateJobObjectW(std::ptr::null(), std::ptr::null());
        if h.is_null() {
            return None;
        }
        let mut info: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = std::mem::zeroed();
        info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
        let ok = SetInformationJobObject(
            h,
            JobObjectExtendedLimitInformation,
            &info as *const _ as *const _,
            std::mem::size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32,
        );
        if ok == 0 {
            CloseHandle(h);
            return None;
        }
        Some(Job(h))
    })
    .as_ref()
}

/// Put a freshly spawned child in the kill-on-close job. Returns false when the
/// job could not be set up or assignment failed (the exit hook still kills it).
pub fn bind_to_job(child: &Child) -> bool {
    let Some(j) = job() else { return false };
    unsafe { AssignProcessToJobObject(j.0, child.as_raw_handle() as HANDLE) != 0 }
}

/// PID of the process listening on 127.0.0.1/0.0.0.0 `port` (IPv4), if any.
pub fn listener_pid(port: u16) -> Option<u32> {
    unsafe {
        let mut size: u32 = 0;
        GetExtendedTcpTable(
            std::ptr::null_mut(),
            &mut size,
            0,
            AF_INET as u32,
            TCP_TABLE_OWNER_PID_LISTENER,
            0,
        );
        if size == 0 {
            return None;
        }
        let mut buf = vec![0u8; size as usize];
        let rc = GetExtendedTcpTable(
            buf.as_mut_ptr() as *mut _,
            &mut size,
            0,
            AF_INET as u32,
            TCP_TABLE_OWNER_PID_LISTENER,
            0,
        );
        if rc != 0 {
            return None;
        }
        let count = u32::from_ne_bytes(buf[0..4].try_into().ok()?) as usize;
        let rows = buf.as_ptr().add(4) as *const MIB_TCPROW_OWNER_PID;
        for i in 0..count {
            let row = std::ptr::read_unaligned(rows.add(i));
            // dwLocalPort holds the port in network byte order in its low 16 bits.
            if u16::from_be(row.dwLocalPort as u16) == port {
                return Some(row.dwOwningPid);
            }
        }
        None
    }
}

/// Full image path of process `pid`, if it can be queried.
pub fn process_path(pid: u32) -> Option<PathBuf> {
    unsafe {
        let h = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if h.is_null() {
            return None;
        }
        let mut buf = vec![0u16; 1024];
        let mut len = buf.len() as u32;
        let ok = QueryFullProcessImageNameW(h, 0, buf.as_mut_ptr(), &mut len);
        CloseHandle(h);
        if ok == 0 {
            return None;
        }
        Some(PathBuf::from(String::from_utf16_lossy(&buf[..len as usize])))
    }
}

/// Terminate `pid` and wait briefly for it to go away.
pub fn kill_pid(pid: u32) {
    use windows_sys::Win32::System::Threading::{TerminateProcess, WaitForSingleObject, PROCESS_SYNCHRONIZE, PROCESS_TERMINATE};
    unsafe {
        let h = OpenProcess(PROCESS_TERMINATE | PROCESS_SYNCHRONIZE, 0, pid);
        if h.is_null() {
            return;
        }
        TerminateProcess(h, 1);
        WaitForSingleObject(h, 5000);
        CloseHandle(h);
    }
}
