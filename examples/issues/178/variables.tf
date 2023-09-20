variable "postgres_image" {
  description = "Which postgres docker image to use."
  default     = "postgres:15"
  type        = string
  sensitive   = false
}

variable "POSTGRES_USER" {
  default   = "postgres"
  type      = string
  sensitive = false
}

variable "POSTGRES_PASSWORD" {
  description = "Password for docker POSTGRES_USER"
  default     = "postgres"
  type        = string
  sensitive   = false
}

variable "POSTGRES_HOST" {
  default   = "127.0.0.1"
  type      = string
  sensitive = false
}

variable "POSTGRES_PORT" {
  description = "Which port postgres should listen on."
  default     = 5432
  type        = number
  sensitive   = false
}

variable "keep_image" {
  description = "If true, then the Docker image won't be deleted on destroy operation. If this is false, it will delete the image from the docker local storage on destroy operation."
  default     = true
  type        = bool
  sensitive   = false
}
