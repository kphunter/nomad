# Nomad ECS Remote Task Driver E2E
resource "aws_ecs_cluster" "nomad_rtd_e2e" {
  name = "nomad-rtd-e2e"
}

resource "aws_ecs_task_definition" "nomad_rtd_e2e" {
  family                   = "nomad-rtd-e2e"
  container_definitions    = file("ecs-task.json")

  # Don't need a network for e2e tests
  network_mode             = "awsvpc"

  requires_compatibilities = ["FARGATE"]
  cpu                      = 256
  memory                   = 512
}
