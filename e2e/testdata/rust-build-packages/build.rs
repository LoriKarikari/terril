use std::process::Command;

fn main() {
    let out = Command::new("protoc").arg("--version").output().unwrap();
    println!("cargo:rustc-env=PROTOC_VERSION={}", String::from_utf8(out.stdout).unwrap().trim());
}
