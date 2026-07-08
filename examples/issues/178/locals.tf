locals {
  read_only_users  = toset([for i in range(var.user_ro_count) : "user_ro_${i}"])
  read_write_users = toset([for i in range(var.user_rw_count) : "user_rw_${i}"])
}
