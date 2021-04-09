variable "name" {}

variable "cidr_block" {
  default = "192.168.1.0/24"
}

variable "availability_zone" {
  default = "eu-central-1a"
}

data "aws_availability_zones" "available" {}

resource aws_vpc "this" {
  cidr_block           = var.cidr_block
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name = var.name
  }
}

resource aws_subnet "public" {
  count = length(data.aws_availability_zones.available.names)

  vpc_id = aws_vpc.this.id
  cidr_block = cidrsubnet(
    cidrsubnet(var.cidr_block, 1, 1), 2, count.index,
  )

  availability_zone = data.aws_availability_zones.available.names[count.index]

  map_public_ip_on_launch = true
}

resource aws_internet_gateway "this" {
  vpc_id = aws_vpc.this.id
}

resource aws_route_table "public_subnets" {
  vpc_id = aws_vpc.this.id
}

resource aws_route "default_via_internet_gateway" {
  route_table_id         = aws_route_table.public_subnets.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.this.id
}

resource aws_route_table_association "public_via_internet_gateway" {
  count = length(aws_subnet.public)

  subnet_id      = element(aws_subnet.public.*.id, count.index)
  route_table_id = aws_route_table.public_subnets.id
}

resource aws_main_route_table_association "this" {
  vpc_id         = aws_vpc.this.id
  route_table_id = aws_route_table.public_subnets.id
}

output "id" {
  value = aws_vpc.this.id
}

output "public_subnet_ids" {
  value = aws_subnet.public.*.id
}
