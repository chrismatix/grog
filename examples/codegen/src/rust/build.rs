fn main() -> std::io::Result<()> {
    println!("cargo:rerun-if-changed=../protobuf/person.proto");
    prost_build::compile_protos(&["../protobuf/person.proto"], &["../protobuf"])?;
    Ok(())
}
