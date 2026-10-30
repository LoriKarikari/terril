use std::io::{Read, Write};
use std::net::TcpListener;

fn main() {
    let port = std::env::var("PORT").unwrap_or_else(|_| "8080".into());
    let listener = TcpListener::bind(("0.0.0.0", port.parse::<u16>().unwrap())).unwrap();
    for stream in listener.incoming() {
        let mut stream = stream.unwrap();
        let mut buf = [0; 1024];
        let _ = stream.read(&mut buf);
        let body = greeting::greet("cargo");
        let _ = write!(stream, "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}", body.len(), body);
    }
}
