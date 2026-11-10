resource "aws_db_instance" "main" {
  engine              = "postgres"
  instance_class      = "db.t3.micro"
  allocated_storage   = 20
  publicly_accessible = false
}

resource "aws_s3_bucket" "uploads" {
  bucket = "example-notes-uploads"
}

resource "aws_lb" "public" {
  name               = "notes"
  load_balancer_type = "application"
  internal           = false
}

resource "aws_instance" "worker" {
  ami           = "ami-0123456789abcdef0"
  instance_type = "t3.micro"
}
