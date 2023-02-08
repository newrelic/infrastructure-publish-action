variable "function_name" {
  default = "infrastructure-s3-matadata-updater"
}
variable "handler_name" {
  default = "lambda"
}
variable "runtime" {
  default = "gp1.x"
}
variable "timeout" {
  default = "10"
}
variable "aws_s3_bucket_id" {
  default = ""
}



#resource "aws_cloudwatch_log_group" "example" {
#  name              = "/aws/lambda/${var.function_name}"
#  retention_in_days = 14
#}


#########################################
# Creating Lambda IAM resource
#########################################

resource "aws_iam_role" "lambda_iam" {
  name = "${var.function_name}-role"

  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "lambda.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
EOF
}

resource "aws_iam_role_policy" "revoke_keys_role_policy" {
  name = "${var.function_name}-policy"
  role = aws_iam_role.lambda_iam.id

  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:ListBucketVersions",
        "s3:ListBucket"
      ],
      "Effect": "Allow",
      "Resource": "*"
    },
    {
      "Sid": "VisualEditor1",
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": ${cloudwatch_log-group}
    },
  ]
}
EOF
}

#########################################
# State Backend
#########################################
terraform {
  backend "s3" {
    bucket = "automation-pipeline-terraform-state"
    key    = "infrastructure-publish-action/lambda"
    region = "us-east-2"
  }
}

##########################################
# Creating Lambda resource
##########################################

resource "aws_lambda_function" "test_lambda" {
  function_name    = var.function_name
  role             = aws_iam_role.lambda_iam.arn
  handler          = "src/${var.handler_name}.lambda_handler"
  runtime          = var.runtime
  timeout          = var.timeout
  filename         = "../lambda.zip"
  source_code_hash = filebase64sha256("../lambda.zip")
  tags = {
    owning_team = "CAOS"
  }
}

# Adding S3 bucket as trigger to my lambda and giving the permissions
resource "aws_s3_bucket_notification" "aws-lambda-trigger" {
  for_each = toset( ["infrastructure_agent/linux/apt/dists/"] )

  bucket = var.aws_s3_bucket_id
  lambda_function {
    lambda_function_arn = aws_lambda_function.test_lambda.arn
    events              = ["s3:ObjectCreated:Put", "s3:ObjectCreated:Post"]
    filter_prefix       = each.key
  }
}
