variable "region" {
    type = string 
}
variable "account_id" {
    type = string 
}
variable "env" {
    type = string 
}
variable "app_cpu" {
    type = number
}
variable "app_memory" {
    type = number
}
variable "compute_node_efs_id" {
    type = string
}
variable "app_slug" {
    type = string
}
variable "source_url" {
    type = string
}
variable "run_on_gpu" {
    type = bool
    default = false
}