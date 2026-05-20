use format::{shout, whisper};

pub fn loud_greeting(name: &str) -> String {
    shout(&format!("hello {}", name))
}

pub fn quiet_greeting(name: &str) -> String {
    whisper(&format!("hello {}", name))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn loud_greeting_shouts() {
        assert_eq!(loud_greeting("grug"), "HELLO GRUG!");
    }

    #[test]
    fn quiet_greeting_whispers() {
        assert_eq!(quiet_greeting("grug"), "(hello grug)");
    }
}
