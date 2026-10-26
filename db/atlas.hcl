variable "database_url" {
  type    = string
  default = getenv("DATABASE_URL")
}

env "local" {
  url = var.database_url
  dev = "docker://postgres/16/dev?search_path=public"
  migration {
    dir = "file://db/migrations"
  }
}

env "dev" {
  url = var.database_url
  dev = "docker://postgres/16/dev?search_path=public"
  migration {
    dir = "file://db/migrations"
  }
}

env "test" {
  url = var.database_url
  dev = "docker://postgres/16/dev?search_path=public"
  migration {
    dir = "file://db/migrations"
  }
}

env "prod" {
  url = var.database_url
  migration {
    dir = "file://db/migrations"
  }
}
