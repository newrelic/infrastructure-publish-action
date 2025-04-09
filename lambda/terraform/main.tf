variable "function_name" {
  default = "infrastructure-s3-matadata-updater"
}
variable "handler_name" {
  default = "lambda"
}

variable "runtime" {
  default = "go1.x"
}

variable "timeout" {
  default = "10"
}

variable "aws_s3_bucket_id" {
  default = "caos-fastly-lambda-test"
}

variable "aws_s3_bucket_arn" {
  default = "arn:aws:s3:::caos-fastly-lambda-test"
}


variable "s3_notifications" {
  default = {
    bucket = {
      name    = "caos-fastly-lambda-test"
      filters = [
        {
          name          = "apt metadata"
          filter_prefix = "infrastructure_agent/linux/apt/dists/"
          filter_suffix = ""
        },
        {
          name          = "yum rpm data xml"
          filter_prefix = "infrastructure_agent/linux/yum/"
          filter_suffix = "xml"
        },
        {
          name          = "yum rpm data xml.asc"
          filter_prefix = "infrastructure_agent/linux/yum/"
          filter_suffix = "asc"
        },
        {
          name          = "yum rpm data bz2"
          filter_prefix = "infrastructure_agent/linux/yum/"
          filter_suffix = "bz2"
        },
        {
          name          = "yum rpm data bz2"
          filter_prefix = "infrastructure_agent/linux/yum/"
          filter_suffix = "gz"
        },
        {
          name          = "zypp rpm data xml"
          filter_prefix = "infrastructure_agent/linux/zypp/"
          filter_suffix = "xml"
        },
        {
          name          = "zypp rpm data xml.asc"
          filter_prefix = "infrastructure_agent/linux/zypp/"
          filter_suffix = "asc"
        },
        {
          name          = "zypp rpm data bz2"
          filter_prefix = "infrastructure_agent/linux/zypp/"
          filter_suffix = "bz2"
        },
        {
          name          = "zypp rpm data bz2"
          filter_prefix = "infrastructure_agent/linux/zypp/"
          filter_suffix = "gz"
        },
      ]
    }
  }
}


#########################################
# Set AWS Provider region
#########################################

provider "aws" {
  region = "us-east-1"
}


resource "aws_cloudwatch_log_group" "lambda_log_group" {
  name              = "/aws/lambda/${var.function_name}"
  retention_in_days = 14
}

resource "aws_iam_role_policy_attachment" "function_logging_policy_attachment" {
  role       = aws_iam_role.lambda_iam.id
  policy_arn = aws_iam_policy.lambda_function_policy.arn
}


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

resource "aws_iam_policy" "lambda_function_policy" {
  name = "${var.function_name}-policy"

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
      "Resource": "${var.aws_s3_bucket_arn}/infrastructure_agent/linux/*"
    },
    {
      "Sid": "VisualEditor1",
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "${aws_cloudwatch_log_group.lambda_log_group.arn}:*"
    }
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
  handler          = var.handler_name
  runtime          = var.runtime
  timeout          = var.timeout
  filename         = "../lambda.zip"
  source_code_hash = filebase64sha256("../lambda.zip")
  depends_on       = [aws_cloudwatch_log_group.lambda_log_group]
  tags             = {
    owning_team = "CAOS"
  }
}

resource "aws_lambda_permission" "allow_bucket" {
  statement_id  = "AllowExecutionFromS3Bucket"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.test_lambda.arn
  principal     = "s3.amazonaws.com"
  source_arn    = var.aws_s3_bucket_arn
}

# Adding S3 bucket as trigger
resource "aws_s3_bucket_notification" "aws-lambda-trigger-post" {
  for_each = {for item in var.s3_notifications :  item.name => item}
  bucket   = each.value.name

  dynamic "lambda_function" {
    for_each = [
    for item in each.value.filters : {
      suffix = item.filter_suffix
      prefix = item.filter_prefix
    }
    ]

    content {
      lambda_function_arn = aws_lambda_function.test_lambda.arn
      events              = ["s3:ObjectCreated:Put"]
      filter_prefix       = lambda_function.value.prefix
      filter_suffix       = lambda_function.value.suffix
    }
  }

  dynamic "lambda_function" {
    for_each = [
    for item in each.value.filters : {
      suffix = item.filter_suffix
      prefix = item.filter_prefix
    }
    ]

    content {
      lambda_function_arn = aws_lambda_function.test_lambda.arn
      events              = ["s3:ObjectCreated:Post"]
      filter_prefix       = lambda_function.value.prefix
      filter_suffix       = lambda_function.value.suffix
    }
  }

  depends_on = [aws_lambda_permission.allow_bucket]
}
