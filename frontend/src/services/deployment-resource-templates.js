export const RESOURCE_TEMPLATES = [
  { value: 'deployment', label: 'Deployment', path: 'deployment.yaml', content: (namespace) => `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: ${namespace}
spec:
  replicas: 2
  selector:
    matchLabels:
      app: app
  template:
    metadata:
      labels:
        app: app
    spec:
      containers:
        - name: app
          image: your-image:latest
          ports:
            - containerPort: 8080
` },
  { value: 'service', label: 'Service', path: 'service.yaml', content: (namespace) => `apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: ${namespace}
spec:
  selector:
    app: app
  ports:
    - port: 80
      targetPort: 8080
` },
  { value: 'configmap', label: 'ConfigMap', path: 'configmap.yaml', content: (namespace) => `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-settings
  namespace: ${namespace}
data: {}
` },
  { value: 'secret', label: 'Secret', path: 'secret.yaml', content: (namespace) => `apiVersion: v1
kind: Secret
metadata:
  name: app-secrets
  namespace: ${namespace}
type: Opaque
stringData: {}
` },
  { value: 'pvc', label: 'PersistentVolumeClaim', path: 'pvc.yaml', content: (namespace) => `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: app-data
  namespace: ${namespace}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
` },
  { value: 'ingress', label: 'Ingress', path: 'ingress.yaml', content: (namespace) => `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: app
  namespace: ${namespace}
spec:
  rules:
    - host: app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: app
                port:
                  number: 80
` },
  { value: 'hpa', label: 'HorizontalPodAutoscaler', path: 'hpa.yaml', content: (namespace) => `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: app
  namespace: ${namespace}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: app
  minReplicas: 1
  maxReplicas: 5
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
` },
  { value: 'statefulset', label: 'StatefulSet', path: 'statefulset.yaml', content: (namespace) => `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: app
  namespace: ${namespace}
spec:
  serviceName: app
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    metadata:
      labels:
        app: app
    spec:
      containers:
        - name: app
          image: your-image:latest
` },
  { value: 'job', label: 'Job', path: 'job.yaml', content: (namespace) => `apiVersion: batch/v1
kind: Job
metadata:
  name: app-job
  namespace: ${namespace}
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: app
          image: your-image:latest
` },
  { value: 'cronjob', label: 'CronJob', path: 'cronjob.yaml', content: (namespace) => `apiVersion: batch/v1
kind: CronJob
metadata:
  name: app-cron
  namespace: ${namespace}
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: app
              image: your-image:latest
` },
]

export function emptyDeploymentResource(namespace = '') {
  return {
    id: '', name: 'resource.yaml', path: 'resource.yaml', format: 'yaml',
    content: `apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example${namespace ? `\n  namespace: ${namespace}` : ''}\ndata: {}\n`,
    api_version: '', kind: '', resource_name: '', namespace: '', sort_order: 0,
    version: 0, release_supported: false,
  }
}
