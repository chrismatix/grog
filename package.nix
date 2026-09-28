{
  lib,
  buildGo127Module,
  version ? "dev",
  commit ? "unknown",
}:

buildGo127Module {
  pname = "grog";
  inherit version;
  src = lib.cleanSource ./.;

  vendorHash = "sha256-LQ0qZlAzmQCjqEmpE7+fG3DqCP9EcpTqgi1BYcxJQvo=";
  subPackages = [ "." ];
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
