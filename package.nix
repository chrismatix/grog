{
  lib,
  buildGoModule,
  fetchFromGitHub,
}:

buildGoModule rec {
  pname = "grog";
  version = "0.16.3";
  buildTestBinaries = false;
  doCheck = false;

  src = fetchFromGitHub {
    owner = "chrismatix";
    repo = "grog";
    rev = "v${version}";
    hash = "sha256-oXumxDtxcu1ZYga/3Z3FtyIb74BtG+9EXNjey0KzUFk=";
  };

  vendorHash = "sha256-6JHqGVV+TDBg8V4Q2Cy11+y5XCSWHG36EqwDAwaCXH8=";

  ldflags = [
    "-s" "-w"
    "-X" "main.version=${version}"
    "-X" "main.commit=${src.rev}"
    "-X" "main.buildDate=unknown"
  ];

  meta = {
    description = "Grog is a mono-repo build tool that is agnostic on how you run your build commands, but instead focuses on caching and parallel execution";
    homepage = "https://github.com/chrismatix/grog";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ ];
    mainProgram = "grog";
  };
}
