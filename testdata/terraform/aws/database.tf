resource "aws_db_subnet_group" "main" {
  name       = "shop-db"
  subnet_ids = [aws_subnet.public_a.id, aws_subnet.public_b.id]
}

resource "aws_db_instance" "main" {
  identifier             = "shop-db"
  engine                 = "postgres"
  engine_version         = "16.3"
  instance_class         = "db.t4g.medium"
  allocated_storage      = 50
  username               = "shop"
  password               = var.db_password
  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.app.id]
}

resource "aws_security_group" "app" {
  name   = "shop-app"
  vpc_id = aws_vpc.main.id

  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
}
