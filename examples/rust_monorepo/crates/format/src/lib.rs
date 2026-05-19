pub fn shout(input: &str) -> String {
    format!("{}!", input.to_uppercase())
}

pub fn whisper(input: &str) -> String {
    format!("({})", input.to_lowercase())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shout_uppercases_and_adds_bang() {
        assert_eq!(shout("hello"), "HELLO!");
    }

    #[test]
    fn whisper_lowercases_and_wraps() {
        assert_eq!(whisper("Hello"), "(hello)");
    }
}
