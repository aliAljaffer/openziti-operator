# Publish an app

A `ZitiApp` turns an app into a Ziti service. The operator creates the configs, the service, the bind and dial policies, and the router policies for you.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: team-a
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx:alpine
          ports:
            - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: team-a
spec:
  selector:
    app: web
  ports:
    - name: http
      port: 8080
      targetPort: 8080
```

```sh
kubectl apply -k .
```

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata:
  name: web
  namespace: team-a
spec:
  expose:
    addresses:
      - web.team-a.example.com
    ports:
      - 8080
  targets:
    - address: 10.0.0.5
    - kubernetesService: web
      port: 8080
  allow:
    groups:
      - staff
  entryRouters:
    - edge-1
```

`expose` is what clients dial. `targets` is where the app runs, one Ziti terminator each. `kubernetesService` becomes the address `<service>.<namespace>.svc`, so the Service above has to exist. `cost` decides which target wins; equal costs share the load.

```sh
kubectl -n team-a get ztapp web -o wide
```

`Hosted`, `Dialable`, `RoutePath`, and `Ready` turn true as the pieces appear. Annotate a Service with `ziti.alialjaffer.com/expose: "true"` and the operator writes the ZitiApp instead. See [First app](../../docs/first-app.md).
