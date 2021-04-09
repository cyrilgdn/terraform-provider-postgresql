variable "name" {
  description = "Name of resources (vpc, db instance, ...)"
  default     = "test-cyrildgn"
}

variable "engine_version" {
  default = "13.2"
}

variable "instance_class" {
  default = "db.t3.micro"
}

variable "username" {
  default = "postgres"
}

variable "password" {
  default = "postgrespwd"
}
