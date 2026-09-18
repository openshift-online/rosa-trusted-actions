locals {
  # When vpc_id is provided, skip all VPC resource creation.
  create_vpc = var.vpc_id == ""

  # Resolved IDs — use passed-in values when an existing VPC is supplied, otherwise
  # reference the resources created below. The [0] index is safe because the resources
  # are created (count = 1) exactly when create_vpc is true.
  vpc_id           = local.create_vpc ? aws_vpc.main[0].id : var.vpc_id
  public_subnet_a  = local.create_vpc ? aws_subnet.public_a[0].id : var.public_subnet_ids[0]
  public_subnet_b  = local.create_vpc ? aws_subnet.public_b[0].id : var.public_subnet_ids[1]
  private_subnet_a = local.create_vpc ? aws_subnet.private_a[0].id : var.private_subnet_ids[0]
  # private_subnet_b is only used in Phase 2; fall back to private_subnet_a when only one is supplied.
  private_subnet_b = (
    local.create_vpc
    ? aws_subnet.private_b[0].id
    : (length(var.private_subnet_ids) > 1 ? var.private_subnet_ids[1] : var.private_subnet_ids[0])
  )
}

data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "main" {
  count                = local.create_vpc ? 1 : 0
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags                 = { Name = "${var.app_name}-vpc", Environment = var.environment }
}

resource "aws_internet_gateway" "main" {
  count  = local.create_vpc ? 1 : 0
  vpc_id = aws_vpc.main[0].id
  tags   = { Name = "${var.app_name}-igw" }
}

# Public subnets — ALB (two AZs required)
resource "aws_subnet" "public_a" {
  count                   = local.create_vpc ? 1 : 0
  vpc_id                  = aws_vpc.main[0].id
  cidr_block              = "10.0.0.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.app_name}-public-a" }
}

resource "aws_subnet" "public_b" {
  count                   = local.create_vpc ? 1 : 0
  vpc_id                  = aws_vpc.main[0].id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[1]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.app_name}-public-b" }
}

# Private subnet — EC2 instance (AZ-a, must match EBS volume AZ)
resource "aws_subnet" "private_a" {
  count             = local.create_vpc ? 1 : 0
  vpc_id            = aws_vpc.main[0].id
  cidr_block        = "10.0.10.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "${var.app_name}-private-a" }
}

# Private subnet — Phase 2 Fargate second task (AZ-b); unused in Phase 1
resource "aws_subnet" "private_b" {
  count             = local.create_vpc ? 1 : 0
  vpc_id            = aws_vpc.main[0].id
  cidr_block        = "10.0.11.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags              = { Name = "${var.app_name}-private-b" }
}

resource "aws_eip" "nat" {
  count  = local.create_vpc ? 1 : 0
  domain = "vpc"
  tags   = { Name = "${var.app_name}-nat-eip" }
}

resource "aws_nat_gateway" "main" {
  count         = local.create_vpc ? 1 : 0
  allocation_id = aws_eip.nat[0].id
  subnet_id     = aws_subnet.public_a[0].id
  tags          = { Name = "${var.app_name}-nat" }
  depends_on    = [aws_internet_gateway.main]
}

resource "aws_route_table" "public" {
  count  = local.create_vpc ? 1 : 0
  vpc_id = aws_vpc.main[0].id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main[0].id
  }
  tags = { Name = "${var.app_name}-public-rt" }
}

resource "aws_route_table" "private" {
  count  = local.create_vpc ? 1 : 0
  vpc_id = aws_vpc.main[0].id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.main[0].id
  }
  tags = { Name = "${var.app_name}-private-rt" }
}

resource "aws_route_table_association" "public_a" {
  count          = local.create_vpc ? 1 : 0
  subnet_id      = aws_subnet.public_a[0].id
  route_table_id = aws_route_table.public[0].id
}

resource "aws_route_table_association" "public_b" {
  count          = local.create_vpc ? 1 : 0
  subnet_id      = aws_subnet.public_b[0].id
  route_table_id = aws_route_table.public[0].id
}

resource "aws_route_table_association" "private_a" {
  count          = local.create_vpc ? 1 : 0
  subnet_id      = aws_subnet.private_a[0].id
  route_table_id = aws_route_table.private[0].id
}

resource "aws_route_table_association" "private_b" {
  count          = local.create_vpc ? 1 : 0
  subnet_id      = aws_subnet.private_b[0].id
  route_table_id = aws_route_table.private[0].id
}
