[![Community Project header](https://github.com/newrelic/opensource-website/raw/master/src/images/categories/Community_Project.png)](https://opensource.newrelic.com/oss-category/#community-project)

# Infrastructure publish action

A GitHub Action to publish artifacts from GitHub release assets into an S3 bucket.

## Inputs
| Key                     | Description |
| ---------------         | ----------- |
| `repo_name`             | Github repository name, combination of organization and repository. |
| `app_name`              | Name of the package. |
| `tag`                   | Tag version from GitHub release. |
| `schema`                | Describes the packages to be published: infra-agent, ohi, or nrjmx. |

All keys are required.

## Use Publish Tag

The example demonstrates how to add a job to your existing workflow to upload Infrastructure agents assets to your S3 bucket.

GitHub secrets to be set:

     AWS_SECRET_ACCESS_KEY - Specifies the secret key associated with the access key.
     AWS_ACCESS_KEY_ID - Personal AWS access key.
     AWS_S3_BUCKET - Name of the AWS S3 bucket.

### Example Usage

    name: Publish

    on:
      push:
        branches:
          - s3_publish_packages

    env:
      DOCKER_HUB_ID: ${{ secrets.OHAI_DOCKER_HUB_ID }}
      DOCKER_HUB_PASSWORD: ${{ secrets.OHAI_DOCKER_HUB_PASSWORD }}

    jobs:

      publish:
        name: Publish artifacts to s3
        runs-on: ubuntu-20.04
        steps:
          - uses: actions/checkout@v2
          - name: Login to DockerHub
            uses: docker/login-action@v1
            with:
              username: ${{ env.DOCKER_HUB_ID }}
              password: ${{ env.DOCKER_HUB_PASSWORD }}
          - name: Publish to S3 action
            uses: ./actions/publish
            with:
              aws_secret_access_key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
              aws_access_key_id: ${{ secrets.AWS_ACCESS_KEY_ID }}
              aws_s3_bucket_name: ${{ secrets.AWS_S3_BUCKET }}
              tag: "v1.0.0"
              app_name: "my-app"
              repo_name: "my-org/my-app"
              schema: "ohi"


## Consistency

As GitHub Actions can run many workflows in parallel, once a publish-action is called it execute a lock mechanism in S3 to avoid conflicts.

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
