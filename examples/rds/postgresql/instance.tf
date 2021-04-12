module "vpc" {
  source = "../vpc/"
  name   = var.name
}

resource "aws_db_subnet_group" "public" {
  name       = var.name
  subnet_ids = module.vpc.public_subnet_ids
}

resource "aws_security_group" "postgresql" {
  name   = "${var.name}-postgresql"
  vpc_id = module.vpc.id

  ingress {
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_db_instance" "db" {
  identifier = var.name
  engine     = "postgres"

  engine_version             = var.engine_version
  auto_minor_version_upgrade = false

  instance_class = var.instance_class

  allocated_storage = 20

  username = var.username
  password = var.password

  skip_final_snapshot = true

  vpc_security_group_ids = [
    aws_security_group.postgresql.id,
  ]

  db_subnet_group_name = aws_db_subnet_group.public.name
  multi_az             = false

  publicly_accessible = true
}

output "db" {
  value = aws_db_instance.db
}
