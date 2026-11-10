resource "aws_s3_bucket" "assets" {
  bucket = "shop-assets-prod"
}

resource "aws_s3_bucket_versioning" "assets" {
  bucket = aws_s3_bucket.assets.id

  versioning_configuration {
    status = "Enabled"
  }
}
