resource "aws_lb" "main" {
  name               = "shop-alb"
  internal           = false
  load_balancer_type = "application"
  subnets            = [aws_subnet.public_a.id, aws_subnet.public_b.id]
  security_groups    = [aws_security_group.alb.id]
}

resource "aws_lb_target_group" "app" {
  name     = "shop-app"
  port     = 8080
  protocol = "HTTP"
  vpc_id   = aws_vpc.main.id
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.main.arn
  port              = 443
  protocol          = "HTTPS"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app.arn
  }
}

resource "aws_ecs_cluster" "main" {
  name = "shop"
}

resource "aws_ecs_task_definition" "app" {
  family                   = "shop-app"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "512"
  memory                   = "1024"
}

resource "aws_ecs_service" "app" {
  name            = "shop-app"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = 2
  launch_type     = "FARGATE"

  load_balancer {
    target_group_arn = aws_lb_target_group.app.arn
    container_name   = "app"
    container_port   = 8080
  }

  depends_on = [aws_lb_listener.https]
}
