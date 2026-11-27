#![feature(test)]

extern crate test;

use std::io::{Read, Write};
use std::net::TcpListener;

fn main() {
    let port = std::env::var("PORT").unwrap_or_else(|_| "8080".into());
    let listener = TcpListener::bind(("0.0.0.0", port.parse().unwrap())).unwrap();
    for stream in listener.incoming() {
        let mut stream = stream.unwrap();
        let _ = stream.read(&mut [0; 1024]);
        let body = concat!("built with ", env!("RUSTC_VERSION"));
        let body = test::black_box(body);
        let _ = write!(stream, "HTTP/1.1 200 OK\r\ncontent-length: {}\r\n\r\n{}", body.len(), body);
    }
}
