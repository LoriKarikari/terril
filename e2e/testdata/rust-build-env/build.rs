use std::process::Command;

fn main() {
    let rustc = std::env::var("RUSTC").unwrap();
    let out = Command::new(rustc).arg("--version").output().unwrap();
    println!("cargo:rustc-env=RUSTC_VERSION={}", String::from_utf8(out.stdout).unwrap().trim());
}
