# partitioner-webhook

Simple webhook to add `podAntiAffinity`, `nodeSelector` and `topologySpreadConstraints` to PODs based on namespace annotations.

## Installation

Create the namespace where the webhook will be deployed:

```bash
$ kubectl create ns partitioner-webhook
```

Generate a private key and certificate for the webhook:

```bash
$ openssl genrsa -out tls.key 4096
$ openssl req -new -key tls.key -x509 -out tls.crt -subj '/CN=partitioner-webhook.partitioner-webhook.svc' -days 7300
```

Import these into a secret:

```bash
$ kubectl -n partitioner-webhook create secret tls partitioner-webhook-tls --cert=tls.crt --key=tls.key
```

Start the webhook deployment:

```bash
$ kubectl -n partitioner-webhook apply -f https://raw.githubusercontent.com/rkojedzinszky/partitioner-webhook/master/deploy/deploy.yml
```

Then, create the webhook:

```bash
$ wget https://raw.githubusercontent.com/rkojedzinszky/partitioner-webhook/master/deploy/webhook.yml
```

Get the certificate encoded in base64:

```bash
$ base64 < tls.crt | tr -d '\n'
```

And paste this in the webhook.yml, assign it to `caBundle`. Then, apply this file too.

```bash
$ kubectl -n partitioner-webhook apply -f webhook.yml
```

## Usage

For each namespace where you want this webhook to activate, a label should be placed. Avoid placing this label on the `partitioner-webhook` itself, as that could end up in a deadlock.

Labelling the namespace:

```bash
$ kubectl label namespace default partitioner=true
```

Then, any pod being created in this namespace will have the additional properties from the namespace's annotations.

For the possible annotations, see [webhook.go](webhook.go#L22)
