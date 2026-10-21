plugins { id("demo.convention") }

dependencies {
    implementation(projects.coreUtils)
    implementation(projects.libs.api.dependencyProject)
    testImplementation(project(":feature:home"))
    implementation(project(path = ":app"))
}
