plugins { id("demo.convention") apply false }

tasks.register("all") { dependsOn(project(":app").tasks) }
