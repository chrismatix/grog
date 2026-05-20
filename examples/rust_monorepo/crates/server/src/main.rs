use greet::quiet_greeting;
use std::io::{BufRead, BufReader, Write};
use std::net::TcpListener;

fn main() -> std::io::Result<()> {
    let addr = std::env::var("ADDR").unwrap_or_else(|_| "127.0.0.1:8080".to_string());
    let listener = TcpListener::bind(&addr)?;
    eprintln!("greet-server listening on http://{}", addr);

    for stream in listener.incoming() {
        let mut stream = stream?;
        let mut reader = BufReader::new(stream.try_clone()?);
        let mut request_line = String::new();
        reader.read_line(&mut request_line)?;

        let name = request_line
            .split_whitespace()
            .nth(1)
            .and_then(|p| p.strip_prefix("/hello/"))
            .unwrap_or("world")
            .to_string();

        let body = quiet_greeting(&name);
        let response = format!(
            "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nContent-Type: text/plain\r\n\r\n{}",
            body.len(),
            body
        );
        stream.write_all(response.as_bytes())?;
    }
    Ok(())
}
