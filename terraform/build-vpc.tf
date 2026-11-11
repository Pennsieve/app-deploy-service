###############################################################################
# The image builder's own VPC (image_builder_in_vpc; docs/builds.md         #
# "Isolation", egress). Builds run app authors' Dockerfiles, so they get a    #
# network of their own: private subnets behind one NAT gateway, an S3 gateway #
# endpoint (image layers and the BuildKit tarball's mirror never pay NAT),    #
# a security group that allows only HTTP, HTTPS and DNS out, and a Route 53   #
# Resolver DNS Firewall that resolves only an allow-list of domains and logs  #
# every query. A build therefore cannot reach the platform VPC (the RDS       #
# proxy, internal load balancers), cannot scan or mine on other ports, and    #
# cannot resolve a host outside the list; what it tries is in the query log.  #
#                                                                             #
# DNS Firewall is a VPC-wide setting, which is why builds do not share the    #
# platform VPC: the allow-list would apply to every service there.           #
###############################################################################

locals {
  build_vpc_enabled = local.use_codebuild && var.image_builder_in_vpc
  build_vpc_name    = "${var.environment_name}-${var.service_name}-build-vpc-${local.region_shortname}"
}

data "aws_availability_zones" "build_vpc" {
  count = local.build_vpc_enabled ? 1 : 0
  state = "available"
}

resource "aws_vpc" "build" {
  count = local.build_vpc_enabled ? 1 : 0

  cidr_block           = var.image_builder_vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = merge(local.common_tags, { Name = local.build_vpc_name })
}

resource "aws_internet_gateway" "build" {
  count  = local.build_vpc_enabled ? 1 : 0
  vpc_id = aws_vpc.build[0].id
  tags   = merge(local.common_tags, { Name = "${local.build_vpc_name}-igw" })
}

# One public subnet for the NAT gateway; two private subnets for builds (two
# AZs, so a build still starts when one is short of capacity).
resource "aws_subnet" "build_public" {
  count = local.build_vpc_enabled ? 1 : 0

  vpc_id            = aws_vpc.build[0].id
  cidr_block        = cidrsubnet(var.image_builder_vpc_cidr, 4, 0)
  availability_zone = data.aws_availability_zones.build_vpc[0].names[0]

  tags = merge(local.common_tags, { Name = "${local.build_vpc_name}-public" })
}

resource "aws_subnet" "build_private" {
  count = local.build_vpc_enabled ? 2 : 0

  vpc_id            = aws_vpc.build[0].id
  cidr_block        = cidrsubnet(var.image_builder_vpc_cidr, 4, count.index + 1)
  availability_zone = data.aws_availability_zones.build_vpc[0].names[count.index]

  tags = merge(local.common_tags, { Name = "${local.build_vpc_name}-private-${count.index}" })
}

resource "aws_eip" "build_nat" {
  count  = local.build_vpc_enabled ? 1 : 0
  domain = "vpc"
  tags   = merge(local.common_tags, { Name = "${local.build_vpc_name}-nat" })
}

resource "aws_nat_gateway" "build" {
  count = local.build_vpc_enabled ? 1 : 0

  allocation_id = aws_eip.build_nat[0].id
  subnet_id     = aws_subnet.build_public[0].id
  depends_on    = [aws_internet_gateway.build]

  tags = merge(local.common_tags, { Name = "${local.build_vpc_name}-nat" })
}

resource "aws_route_table" "build_public" {
  count  = local.build_vpc_enabled ? 1 : 0
  vpc_id = aws_vpc.build[0].id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.build[0].id
  }

  tags = merge(local.common_tags, { Name = "${local.build_vpc_name}-public" })
}

resource "aws_route_table_association" "build_public" {
  count          = local.build_vpc_enabled ? 1 : 0
  subnet_id      = aws_subnet.build_public[0].id
  route_table_id = aws_route_table.build_public[0].id
}

resource "aws_route_table" "build_private" {
  count  = local.build_vpc_enabled ? 1 : 0
  vpc_id = aws_vpc.build[0].id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.build[0].id
  }

  tags = merge(local.common_tags, { Name = "${local.build_vpc_name}-private" })
}

resource "aws_route_table_association" "build_private" {
  count          = local.build_vpc_enabled ? 2 : 0
  subnet_id      = aws_subnet.build_private[count.index].id
  route_table_id = aws_route_table.build_private[0].id
}

# S3 through the gateway endpoint: ECR serves image layers from S3, so
# pulls and pushes do not pay for NAT data.
resource "aws_vpc_endpoint" "build_s3" {
  count = local.build_vpc_enabled ? 1 : 0

  vpc_id            = aws_vpc.build[0].id
  service_name      = "com.amazonaws.${data.aws_region.current_region.name}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.build_private[0].id]

  tags = merge(local.common_tags, { Name = "${local.build_vpc_name}-s3" })
}

# Out: HTTP and HTTPS anywhere (package indexes, code hosts, registries), DNS
# to the VPC resolver. Nothing else, and nothing in.
resource "aws_security_group" "image_builder" {
  count = local.build_vpc_enabled ? 1 : 0

  name        = "${var.environment_name}-${var.service_name}-image-builder-${local.region_shortname}"
  description = "App Store image builds: HTTP, HTTPS and DNS out only"
  vpc_id      = aws_vpc.build[0].id

  egress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    description = "HTTP (distribution package mirrors)"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    description = "DNS to the VPC resolver"
    from_port   = 53
    to_port     = 53
    protocol    = "udp"
    cidr_blocks = [var.image_builder_vpc_cidr]
  }
  egress {
    description = "DNS to the VPC resolver (TCP)"
    from_port   = 53
    to_port     = 53
    protocol    = "tcp"
    cidr_blocks = [var.image_builder_vpc_cidr]
  }

  tags = merge(local.common_tags, { Name = "${local.build_vpc_name}-image-builder" })
}

# --- DNS Firewall: resolve the allow-list, block the rest, log everything ---

resource "aws_route53_resolver_firewall_domain_list" "build_allowed" {
  count = local.build_vpc_enabled ? 1 : 0

  name    = "${local.build_vpc_name}-allowed"
  domains = var.image_builder_allowed_domains
  tags    = local.common_tags
}

resource "aws_route53_resolver_firewall_domain_list" "build_all" {
  count = local.build_vpc_enabled ? 1 : 0

  name    = "${local.build_vpc_name}-all"
  domains = ["*"]
  tags    = local.common_tags
}

resource "aws_route53_resolver_firewall_rule_group" "build" {
  count = local.build_vpc_enabled ? 1 : 0

  name = "${local.build_vpc_name}-egress"
  tags = local.common_tags
}

resource "aws_route53_resolver_firewall_rule" "build_allow" {
  count = local.build_vpc_enabled ? 1 : 0

  name                    = "allow-listed"
  action                  = "ALLOW"
  firewall_domain_list_id = aws_route53_resolver_firewall_domain_list.build_allowed[0].id
  firewall_rule_group_id  = aws_route53_resolver_firewall_rule_group.build[0].id
  priority                = 100
}

resource "aws_route53_resolver_firewall_rule" "build_block_rest" {
  count = local.build_vpc_enabled ? 1 : 0

  name                    = "block-everything-else"
  action                  = "BLOCK"
  block_response          = "NXDOMAIN"
  firewall_domain_list_id = aws_route53_resolver_firewall_domain_list.build_all[0].id
  firewall_rule_group_id  = aws_route53_resolver_firewall_rule_group.build[0].id
  priority                = 200
}

resource "aws_route53_resolver_firewall_rule_group_association" "build" {
  count = local.build_vpc_enabled ? 1 : 0

  name                   = "${local.build_vpc_name}-egress"
  firewall_rule_group_id = aws_route53_resolver_firewall_rule_group.build[0].id
  vpc_id                 = aws_vpc.build[0].id
  priority               = 101
  # Fail closed: when the firewall cannot evaluate a query, it is blocked.
  mutation_protection = "DISABLED"
  tags                = local.common_tags
}

resource "aws_route53_resolver_firewall_config" "build" {
  count = local.build_vpc_enabled ? 1 : 0

  resource_id        = aws_vpc.build[0].id
  firewall_fail_open = "DISABLED"
}

# Every query the build VPC makes, allowed or blocked, with the rule that
# decided it: what a build tried to reach.
resource "aws_cloudwatch_log_group" "build_dns" {
  count = local.build_vpc_enabled ? 1 : 0

  name              = "/aws/route53resolver/${local.build_vpc_name}"
  retention_in_days = 90
  tags              = local.common_tags
}

resource "aws_route53_resolver_query_log_config" "build" {
  count = local.build_vpc_enabled ? 1 : 0

  name            = "${local.build_vpc_name}-queries"
  destination_arn = aws_cloudwatch_log_group.build_dns[0].arn
  tags            = local.common_tags
}

resource "aws_route53_resolver_query_log_config_association" "build" {
  count = local.build_vpc_enabled ? 1 : 0

  resolver_query_log_config_id = aws_route53_resolver_query_log_config.build[0].id
  resource_id                  = aws_vpc.build[0].id
}

# Flow logs: what a build reached by IP (the one thing DNS Firewall cannot
# see). Rejected traffic only, to keep the volume small.
resource "aws_cloudwatch_log_group" "build_flow" {
  count = local.build_vpc_enabled ? 1 : 0

  name              = "/aws/vpc/${local.build_vpc_name}-flow"
  retention_in_days = 30
  tags              = local.common_tags
}

resource "aws_iam_role" "build_flow_logs" {
  count = local.build_vpc_enabled ? 1 : 0

  name = "${local.build_vpc_name}-flow-logs"
  assume_role_policy = jsonencode({
    Version   = "2012-10-17"
    Statement = [{ Effect = "Allow", Principal = { Service = "vpc-flow-logs.amazonaws.com" }, Action = "sts:AssumeRole" }]
  })
  tags = local.common_tags
}

resource "aws_iam_role_policy" "build_flow_logs" {
  count = local.build_vpc_enabled ? 1 : 0

  name = "${local.build_vpc_name}-flow-logs"
  role = aws_iam_role.build_flow_logs[0].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["logs:CreateLogStream", "logs:PutLogEvents", "logs:DescribeLogGroups", "logs:DescribeLogStreams"]
      Resource = "${aws_cloudwatch_log_group.build_flow[0].arn}:*"
    }]
  })
}

resource "aws_flow_log" "build" {
  count = local.build_vpc_enabled ? 1 : 0

  vpc_id          = aws_vpc.build[0].id
  traffic_type    = "ALL"
  log_destination = aws_cloudwatch_log_group.build_flow[0].arn
  iam_role_arn    = aws_iam_role.build_flow_logs[0].arn
  tags            = local.common_tags
}
