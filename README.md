[![Community Project header](https://github.com/newrelic/opensource-website/raw/master/src/images/categories/Community_Project.png)](https://opensource.newrelic.com/oss-category/#community-project)

# Infrastructure publish action

A GitHub Action to publish artifacts from GitHub release assets into an S3 bucket.

## Inputs
| Key                        | Description |
| ---------------            | ----------- |
| `disable_lock`             | Disabled locking, for stuff that won't need one, like Windows MSIs. |
| `run_id`                   | Action run identifier. To be provisioned from env-var `GITHUB_RUN_ID`. |
| `aws_s3_lock_bucket_name`  | Name of the S3 bucket for lockfiles. |
| `aws_role_session_name`    | Name of the S3 role session name. |
| `aws_region`               | AWS region for the buckets. |
| `aws_role_arn`             | ARN for the IAM role to be used for fetching AWS STS credentials. |
| `aws_secret_access_key`    | AWS secret access key. |
| `aws_access_key_id`        | AWS access key id. |
| `aws_s3_bucket_name`       | Name of the S3 bucket. |
| `repo_name`                | Github repository name, combination of organization and repository. |
| `app_name`                 | Name of the package. |
| `tag`                      | Tag version from GitHub release. |
| `schema`                   | Describes the packages to be published: infra-agent, ohi, or nrjmx. |
| `schema_url`               | Url to custom schema file. |
| `gpg_passphrase`           | Passphrase for the gpg key. |
| `gpg_private_key_base64`   | Encoded gpg key. |

All keys are required.

## Use Publish Tag

The example demonstrates how to add a job to your existing workflow to upload Infrastructure agents assets to your S3 bucket.

GitHub secrets to be set:

     AWS_SECRET_ACCESS_KEY - Specifies the secret key associated with the access key.
     AWS_ACCESS_KEY_ID - Personal AWS access key.
     AWS_S3_BUCKET - Name of the AWS S3 bucket.

### Example Usage

```yaml
name: Publish

on:
  release:
    types:
      - released
    tags:
      - '*'

env:
  GPG_PASSPHRASE: ${{ secrets.OHAI_GPG_PASSPHRASE }}
  GPG_PRIVATE_KEY_BASE64: ${{ secrets.OHAI_GPG_PRIVATE_KEY_BASE64 }}
  TAG: ${{ github.event.release.tag_name }}
  DOCKER_HUB_ID: ${{ secrets.OHAI_DOCKER_HUB_ID }}
  DOCKER_HUB_PASSWORD: ${{ secrets.OHAI_DOCKER_HUB_PASSWORD }}
  AWS_S3_BUCKET_NAME: "nr-downloads-main"
  AWS_S3_LOCK_BUCKET_NAME: "onhost-ci-lock"
  AWS_REGION: "us-east-1"

jobs:
  publishing-to-s3-linux:
    name: Publish linux artifacts into s3 bucket
    runs-on: ubuntu-20.04

    steps:
      - name: Login to DockerHub
        uses: docker/login-action@v1
        with:
          username: ${{ env.DOCKER_HUB_ID }}
          password: ${{ env.DOCKER_HUB_PASSWORD }}
      - name: Publish to S3 action
        uses: newrelic/infrastructure-publish-action@main
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID_PROD }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY_PROD }}
          AWS_ROLE_ARN: ${{ secrets.AWS_ROLE_ARN_PROD }}
        with:
          tag: ${{env.TAG}}
          app_name: "newrelic-infra"
          repo_name: "newrelic/infrastructure-agent"
          env: release
          schema: "custom"
          schema_url: "https://raw.githubusercontent.com/newrelic/infrastructure-agent/master/build/upload-schema-linux.yml"
          aws_access_key_id: ${{ env.AWS_ACCESS_KEY_ID }}
          aws_secret_access_key: ${{ env.AWS_SECRET_ACCESS_KEY }}
          aws_s3_bucket_name: ${{ env.AWS_S3_BUCKET_NAME }}
          aws_s3_lock_bucket_name: ${{ env.AWS_S3_LOCK_BUCKET_NAME }}
          run_id: ${{ github.run_id }}
          aws_region: ${{ env.AWS_REGION }}
          aws_role_arn: ${{ env.AWS_ROLE_ARN }}
          # used for signing package stuff
          gpg_passphrase: ${{ env.GPG_PASSPHRASE }}
          gpg_private_key_base64: ${{ env.GPG_PRIVATE_KEY_BASE64 }}
```

## Consistency

As GitHub Actions can run many workflows in parallel, once a publish-action is called it execute a lock mechanism in S3 to avoid conflicts. 
In the current implementation only one publish action with the same lock will be executed, all other concurrent jobs will be terminated by the lock file check.

## Support

If you need assistance with New Relic products, you are in good hands with several support diagnostic tools and support channels.

If the issue has been confirmed as a bug or is a feature request, file a GitHub issue.

**Support Channels**

* [New Relic Documentation](https://docs.newrelic.com): Comprehensive guidance for using our platform
* [New Relic Community](https://discuss.newrelic.com/c/support-products-agents/new-relic-infrastructure): The best place to engage in troubleshooting questions
* [New Relic Developer](https://developer.newrelic.com/): Resources for building a custom observability applications
* [New Relic University](https://learn.newrelic.com/): A range of online training for New Relic users of every level
* [New Relic Technical Support](https://support.newrelic.com/) 24/7/365 ticketed support. Read more about our [Technical Support Offerings](https://docs.newrelic.com/docs/licenses/license-information/general-usage-licenses/support-plan).

## Contribute

We encourage your contributions to improve Infrastructure publish action! Keep in mind that when you submit your pull request, you'll need to sign the CLA via the click-through using CLA-Assistant. You only have to sign the CLA one time per project.

If you have any questions, or to execute our corporate CLA (which is required if your contribution is on behalf of a company), drop us an email at opensource@newrelic.com.

**A note about vulnerabilities**

As noted in our [security policy](../../security/policy), New Relic is committed to the privacy and security of our customers and their data. We believe that providing coordinated disclosure by security researchers and engaging with the security community are important means to achieve our security goals.

If you believe you have found a security vulnerability in this project or any of New Relic's products or websites, we welcome and greatly appreciate you reporting it to New Relic through [HackerOne](https://hackerone.com/newrelic).

If you would like to contribute to this project, review [these guidelines](./CONTRIBUTING.md).

To all contributors, we thank you!  Without your contribution, this project would not be what it is today.  We also host a community project page dedicated to [Project Name](<LINK TO https://opensource.newrelic.com/projects/... PAGE>).

## License
Infrastructure publish action use repolinter-action which is licensed under the [Apache 2.0](http://apache.org/licenses/LICENSE-2.0.txt) License.
