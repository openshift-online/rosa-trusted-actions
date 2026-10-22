resource "aws_db_subnet_group" "main" {
  name       = var.app_name
  subnet_ids = [local.private_subnet_a, local.private_subnet_b]
  tags       = { Name = "${var.app_name}-db-subnet-group", Environment = var.environment }
}

resource "aws_rds_cluster" "main" {
  cluster_identifier = var.app_name
  engine             = "aurora-postgresql"
  engine_version     = var.db_engine_version # pinned; a floating minor means an unplanned failover
  database_name      = var.db_name
  master_username    = var.db_username

  # RDS generates and owns the password. No password in config or state.
  manage_master_user_password = true

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.aurora.id]

  storage_encrypted         = true
  kms_key_id                = var.db_kms_key_id != "" ? var.db_kms_key_id : null
  backup_retention_period   = var.db_backup_retention_period
  preferred_backup_window   = var.db_preferred_backup_window
  deletion_protection       = var.db_deletion_protection
  skip_final_snapshot       = var.db_skip_final_snapshot
  final_snapshot_identifier = var.db_skip_final_snapshot ? null : "${var.app_name}-final"

  tags = { Name = "${var.app_name}-aurora", Environment = var.environment }
}

resource "aws_rds_cluster_instance" "main" {
  count              = var.db_instance_count
  identifier         = "${var.app_name}-${count.index}"
  cluster_identifier = aws_rds_cluster.main.id
  instance_class     = var.db_instance_class
  engine             = aws_rds_cluster.main.engine
  engine_version     = aws_rds_cluster.main.engine_version

  performance_insights_enabled = var.db_performance_insights_enabled

  tags = { Name = "${var.app_name}-aurora-${count.index}", Environment = var.environment }
}
