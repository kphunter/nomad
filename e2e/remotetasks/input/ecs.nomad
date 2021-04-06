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
          network_configuration {
            aws_vpc_configuration {
              assign_public_ip = "ENABLED"

              #FIXME Needs to be dynamic
              security_groups  = ["sg-003ba270530e021f8"]
              subnets          = ["subnet-db25e7bd"]
            }
          }
        }
      }
    }
  }
}
