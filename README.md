# Scrum Poker

This is a simple site with no external dependencies to host a poker like application for scrum estimations.

## Args

`-addr` Server Address (default "0.0.0.0:8080")

`-debug` Enable Debug Logging

`-log-endpoints` Log Endpoints

`-no-color` No Color Output

## Docker

### CLI

```sh
docker pull ghcr.io/joeyak/scrum-poker:master
docker run -p 8080:8080 --rm ghcr.io/joeyak/scrum-poker:master
```

### Docker Compose

```yaml
services:
  scrum-poker:
    image: ghcr.io/joeyak/scrum-poker:master
    restart: unless-stopped
    ports:
      - 80:8080
```

## Nginx

In order to run this behind an nginx proxy, some settings must be set. Here's an example of my nginx config for it, the import parts are the http_version and headers for the proxy pass.

```nginx
# domain.conf
server {
    listen 80;
    server_name sub.domain.com;

    return 302 https://$server_name$request_uri;
}

server {
    listen 443 ssl;
    server_name sub.domain.com;

    ...

    location / {
        proxy_pass http://127.0.0.1:8080;
        include proxy_params;
    }

    location ~* /ws$ {
        proxy_pass http://127.0.0.1:8080;
        include proxy_params_ws;
    }
}

# proxy_params
proxy_set_header Host $http_host;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;

# proxy_params_ws
proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "Upgrade";
proxy_set_header Host $http_host;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
```

## Cloudformation

The `cloudformation.yaml` file can be used to create the application in aws.

```bash
aws cloudformation create-stack --stack-name scrum-poker --template-body file://cloudformation.yaml --capabilities CAPABILITY_IAM --parameters "ParameterKey=CertificateArn,ParameterValue=<CERTIFICATE_ARN>"
```

The default tag version is latest, but if you want to use master, add `"ParameterKey=ImageTag,ParameterValue=master"` to the `--parameters`

If you want to use a different subdomain besides `poker` you can specify it with the paramter `"ParameterKey=SubDomain,ParameterValue=www"`

Run the script below to update the service's image version if there's a new version out.

```bash
aws ecs update-service --cluster <CLUSTER_NAME> --service <SERVICE_NAME> --force-new-deployment
```
