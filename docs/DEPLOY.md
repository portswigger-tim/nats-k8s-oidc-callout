# Deploying nats-k8s-oidc-callout

Guide for deploying the NATS Kubernetes OIDC auth callout service.

## Prerequisites

- Kubernetes cluster (1.20+)
- kubectl configured
- nsc (NATS CLI tools)
- NATS server (2.9+) with auth callout support

## Deployment Steps

### 1. Install NATS Server

```bash
helm repo add nats https://nats-io.github.io/k8s/helm/charts/
helm repo update

kubectl create namespace nats

helm install nats nats/nats \
  --namespace nats \
  --set config.cluster.enabled=true \
  --set config.jetstream.enabled=true
```

### 2. Generate NATS Credentials

```bash
# Create operator and accounts
nsc add operator --name MyOperator
nsc add account --name AUTH_SERVICE
nsc add user --account AUTH_SERVICE --name auth-service
nsc add account --name AUTH_ACCOUNT

# Generate credentials
nsc generate creds --account AUTH_SERVICE --user auth-service > auth-service.creds

# Create Kubernetes secret
kubectl create namespace nats-auth
kubectl create secret generic nats-auth-creds \
  --from-file=auth.creds=auth-service.creds \
  --namespace=nats-auth
```

### 3. Configure Kubernetes RBAC

```yaml
# rbac.yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: nats-auth-callout
  namespace: nats-auth
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: nats-auth-callout
rules:
  - apiGroups: [""]
    resources: ["serviceaccounts"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: nats-auth-callout
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: nats-auth-callout
subjects:
  - kind: ServiceAccount
    name: nats-auth-callout
    namespace: nats-auth
```

Apply:
```bash
kubectl apply -f rbac.yaml
```

### 4. Deploy Auth Service

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nats-auth-callout
  namespace: nats-auth
spec:
  replicas: 2
  selector:
    matchLabels:
      app: nats-auth-callout
  template:
    metadata:
      labels:
        app: nats-auth-callout
    spec:
      serviceAccountName: nats-auth-callout
      containers:
        - name: nats-auth-callout
          image: nats-k8s-oidc-callout:latest
          ports:
            - containerPort: 8080
              name: http
          env:
            - name: NATS_URL
              value: "nats://nats.nats.svc.cluster.local:4222"
            - name: NATS_CREDS_FILE
              value: "/etc/nats/auth.creds"
            - name: NATS_ACCOUNT
              value: "AUTH_ACCOUNT"
            - name: K8S_IN_CLUSTER
              value: "true"
            - name: JWT_AUDIENCE
              value: "nats"
            - name: LOG_LEVEL
              value: "info"
          volumeMounts:
            - name: nats-creds
              mountPath: /etc/nats
              readOnly: true
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 256Mi
      volumes:
        - name: nats-creds
          secret:
            secretName: nats-auth-creds
---
apiVersion: v1
kind: Service
metadata:
  name: nats-auth-callout
  namespace: nats-auth
spec:
  type: ClusterIP
  ports:
    - port: 8080
      targetPort: http
      name: http
  selector:
    app: nats-auth-callout
```

Apply:
```bash
kubectl apply -f deployment.yaml
```

### 5. Configure Client ServiceAccount

```yaml
# client-sa.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-nats-client
  namespace: my-app
  annotations:
    nats.io/allowed-pub-subjects: "my-app.requests.>,my-app.events.>"
    nats.io/allowed-sub-subjects: "my-app.responses.>,my-app.commands.>"
```

Apply:
```bash
kubectl apply -f client-sa.yaml
```

## Verification

### Check Deployment

```bash
# Check pods
kubectl get pods -n nats-auth

# Check logs
kubectl logs -n nats-auth -l app=nats-auth-callout

# Check health
kubectl port-forward -n nats-auth svc/nats-auth-callout 8080:8080
curl http://localhost:8080/healthz
```

### Test Authentication

```bash
# Create test token
kubectl create token my-nats-client \
  --namespace=my-app \
  --audience=nats \
  --duration=1h > test-token.txt

# Test with NATS CLI
nats --server=nats://nats.nats.svc.cluster.local:4222 \
  --token=$(cat test-token.txt) \
  pub my-app.test "hello"
```

### View Metrics

```bash
kubectl port-forward -n nats-auth svc/nats-auth-callout 8080:8080
curl http://localhost:8080/metrics
```

## Troubleshooting

### Connection Issues

```bash
# Verify NATS server is accessible
kubectl run -it --rm debug --image=natsio/nats-box --restart=Never \
  -- nats server check --server=nats://nats.nats.svc.cluster.local:4222
```

### Token Validation Failures

```bash
# Decode token
echo "TOKEN" | jwt decode -

# Verify issuer and audience match configuration
```

### Permission Denials

```bash
# Check ServiceAccount annotations
kubectl get serviceaccount my-nats-client -n my-app -o yaml

# Check auth service logs
kubectl logs -n nats-auth -l app=nats-auth-callout | grep "permission"
```

### JWKS Errors

```bash
# Verify JWKS endpoint is accessible
kubectl exec -it -n nats-auth <pod-name> -- \
  wget -O- https://kubernetes.default.svc/openid/v1/jwks
```

## Configuration Reference

### Required Environment Variables
- `NATS_CREDS_FILE` - Path to NATS credentials
- `NATS_ACCOUNT` - NATS account to assign clients

### Optional (with smart defaults)
- `NATS_URL` - Default: `nats://nats:4222`
- `JWKS_URL` - Default: `https://kubernetes.default.svc/openid/v1/jwks`
- `JWT_ISSUER` - Default: `https://kubernetes.default.svc`
- `JWT_AUDIENCE` - Default: `nats`
- `LOG_LEVEL` - Default: `info`

### ServiceAccount Annotations
- `nats.io/allowed-pub-subjects` - Comma-separated publish subjects
- `nats.io/allowed-sub-subjects` - Comma-separated subscribe subjects

Subject patterns:
- `*` - Single token wildcard
- `>` - Multi-token wildcard (must be last token)

## Production Considerations

### High Availability
- Run 2-3 replicas minimum
- Use pod anti-affinity
- Configure resource requests/limits

### Security
- Use Network Policies
- Regularly rotate NATS credentials
- Monitor unauthorized access attempts
- Use TLS for NATS connections

### Monitoring
- Alert on authentication failures
- Monitor cache hit rates
- Track permission denials
- Create dashboards for key metrics

### Scaling
- Auth service is stateless
- Use HorizontalPodAutoscaler
- Consider connection pooling
- Tune cache for large clusters
