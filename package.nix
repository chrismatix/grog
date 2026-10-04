{
  lib,
  buildGo127Module,
  version ? "dev",
  commit ? "unknown",
  withDuckdb ? false,
}:

buildGo127Module {
  pname = if withDuckdb then "grog-full" else "grog";
  inherit version;
  src = lib.cleanSource ./.;

  vendorHash = "sha256-OyiBJ0b3hdBGOSizWbNgE+ngc2yD4Tm6O2k/SEwu+00=";
  subPackages = [ "." ];
  tags = lib.optional withDuckdb "duckdb";
  doCheck = false;

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
    "-X main.commit=${commit}"
    "-X main.buildDate=unknown"
  ];

  meta = {
    description = "Mono-repo build tool that caches and parallelizes your existing build commands";
    homepage = "https://grog.build";
    license = lib.licenses.mit;
    mainProgram = "grog";
  };
}
