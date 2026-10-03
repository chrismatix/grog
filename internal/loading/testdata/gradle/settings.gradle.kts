rootProject.name = "demo"

pluginManagement {
    includeBuild("build-logic")
}

include(":app", ":core-utils")
include(
    ":libs:api",
    ":feature:home",
)
include("unused")
project(":feature:home").projectDir = file("feature/moved-dir")
