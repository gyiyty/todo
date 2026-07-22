# Temporary direct-IP HTTPS deployment

The temporary browser URL is `https://120.79.29.34:38675`. Clients connect
directly to BT Panel Nginx, which forwards requests to the Todo container on
`127.0.0.1:8787`. Cloudflare, its Origin CA, and port 8443 are not part of this
request path.

The tracked Nginx example is
[`deploy/todo-ip-https.conf.example`](../deploy/todo-ip-https.conf.example). TLS
keys and certificates are deployment state and must never be committed.

## 1. Certificate material

The current host stores generated material in `/home/yunyyyy/todo-tls`:

| File | Purpose |
| --- | --- |
| `ca.cert.pem` | Public root certificate installed on personal clients |
| `ca.key.pem` | Root private key; never distribute or install in Nginx |
| `server.cert.pem` | Server certificate with SAN `IP:120.79.29.34` |
| `server.key.pem` | Server private key installed for Nginx |

Verify the certificate before installation:

```bash
openssl verify -CAfile /home/yunyyyy/todo-tls/ca.cert.pem \
  -verify_ip 120.79.29.34 -purpose sslserver \
  /home/yunyyyy/todo-tls/server.cert.pem
openssl x509 -in /home/yunyyyy/todo-tls/ca.cert.pem -noout \
  -fingerprint -sha256
```

Keep `ca.key.pem` until every client passes acceptance, then destroy it. The
server certificate is intentionally temporary. If the public IP changes, issue
a new private CA and reinstall its public certificate on all clients.

## 2. Nginx installation (manual, privileged)

Install only the server certificate and key into the BT Panel certificate path:

```bash
sudo install -d -m 700 /www/server/panel/vhost/cert/todo-ip
sudo install -m 644 /home/yunyyyy/todo-tls/server.cert.pem \
  /www/server/panel/vhost/cert/todo-ip/server.cert.pem
sudo install -m 600 /home/yunyyyy/todo-tls/server.key.pem \
  /www/server/panel/vhost/cert/todo-ip/server.key.pem
```

In BT Panel, replace the Todo site's configuration with
`deploy/todo-ip-https.conf.example`, then test and reload:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

Do not install the root CA private key in BT Panel. Nginx serves only the leaf
certificate; each client validates it against its locally trusted root.

## 3. Network rules (manual)

In the Alibaba Cloud security group, allow inbound TCP 38675 from `0.0.0.0/0`.
The source must be open because the personal clients do not have fixed public IP
addresses. Then allow the same port in UFW:

```bash
sudo ufw allow 38675/tcp comment 'Todo HTTPS'
sudo ufw status numbered
```

Do not expose TCP 8787. After 38675 passes all external checks, remove the 8443
allow rules from both UFW and the Alibaba Cloud security group. Disable or delete
the old Cloudflare `task` DNS record so it does not continue returning 525.

## 4. Application origin

The ignored production `.env` must contain:

```dotenv
TODO_BASE_URL=https://120.79.29.34:38675
```

Recreate the container so Secure cookies and HSTS use the new origin:

```bash
cd /home/yunyyyy/todo
docker compose up -d --build
docker compose ps
curl -fsS http://127.0.0.1:8787/health/ready
```

## 5. Install the public root on clients

Transfer only `ca.cert.pem` over the existing SSH connection. For example:

```bash
scp -P 22964 <ssh-user>@120.79.29.34:/home/yunyyyy/todo-tls/ca.cert.pem \
  ./todo-personal-root-ca.crt
```

Compare its SHA-256 fingerprint with the value printed in the trusted SSH
session before importing it.

### Windows

Run an elevated terminal:

```powershell
certutil -addstore -f Root .\todo-personal-root-ca.crt
```

Restart the browser. To remove it later, open `certmgr.msc`, find **Personal Todo
Root CA** under **Trusted Root Certification Authorities**, and delete it.

### Arch Linux

Install it in the system p11-kit trust store:

```bash
sudo trust anchor --store ./todo-personal-root-ca.crt
trust list | grep -A3 'Personal Todo Root CA'
```

Restart Chromium. If Chromium or Firefox uses an independent certificate store,
open its certificate settings and import the same file as a trusted authority.
Remove it later with `sudo trust anchor --remove` using the certificate path
reported by `trust list`.

### Android

Copy `todo-personal-root-ca.crt` to the phone. Open **Settings > Security and
privacy > More security settings > Encryption and credentials > Install a
certificate > CA certificate**. Names vary slightly by Android vendor. Select
the file, accept the user-CA warning, and restart Chrome. Remove it later from
**Trusted credentials > User**.

## 6. Acceptance and cleanup

From a trusted client:

```bash
curl --cacert ./todo-personal-root-ca.crt \
  https://120.79.29.34:38675/health/ready
```

The response must be `{"status":"ready"}` without `-k`. Test login and task
create/edit/complete/delete from Windows, Arch, and Android. Also confirm that
public TCP 8443 and 8787 fail while the local 8787 health check remains healthy.

Only after those checks, destroy the retained CA private key. Keep the public CA,
server certificate, and server private key for the lifetime of this deployment.

This is a temporary personal deployment, not a statement that public IP access
is exempt from provider or regulatory requirements. Confirm long-term public
hosting requirements with the cloud provider, or move to a private overlay such
as Tailscale.
