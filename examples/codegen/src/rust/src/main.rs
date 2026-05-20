pub mod proto {
    include!(concat!(env!("OUT_DIR"), "/grog.codegen.rs"));
}

use proto::Person;

fn main() {
    let p = Person {
        name: "Grace Hopper".to_string(),
        id: 1,
        email: "grace@hopper.com".to_string(),
    };
    println!("Name: {}, Id: {}, Email: {}", p.name, p.id, p.email);
}

#[cfg(test)]
mod tests {
    use super::proto::Person;

    #[test]
    fn person_round_trips_fields() {
        let p = Person {
            name: "Grace Hopper".to_string(),
            id: 1,
            email: "grace@hopper.com".to_string(),
        };
        assert_eq!(p.name, "Grace Hopper");
        assert_eq!(p.id, 1);
    }
}
