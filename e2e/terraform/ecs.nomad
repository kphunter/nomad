job "nomad-ecs-e2e" {
  datacenters = ["dc1"]

  group "ecs-remote-task-e2e" {
    restart {
      attempts = 0
      mode     = "fail"
    }

    reschedule {
      delay = "5s"
    }

    task "http-server" {
      driver       = "ecs"
      kill_timeout = "1m" // increased from default to accomodate ECS.

      config {
        task {
          launch_type     = "FARGATE"
          task_definition = "nomad-rtd-e2e"
        }
      }
    }
  }
}
