use greet::loud_greeting;
use std::env;

fn main() {
    let name = env::args().nth(1).unwrap_or_else(|| "world".to_string());
    println!("{}", loud_greeting(&name));
}

#[cfg(test)]
mod tests {
    use greet::loud_greeting;

    #[test]
    fn cli_greeting_is_loud() {
        assert!(loud_greeting("grug").contains("GRUG"));
    }
}
