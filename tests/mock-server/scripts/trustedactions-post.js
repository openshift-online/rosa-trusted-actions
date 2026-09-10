// keyed {clusterId}:{instanceId}, holding the granted request body for the proxy to read
var actionStore = stores.open('actions');

var clusterId = context.request.pathParams.clusterId;
var body = JSON.parse(context.request.body);

// body example:
// {
//   "name": "my-trusted-action",
//   "customerDataAccess": false,
//   "rbac": {
//     "clusterRoleRules": [
//       {
//         "verbs": ["get", "list", "watch"],
//         "apiGroups": [""],
//         "resources": ["nodes", "pods"]
//       }
//     ],
//     "roles": [
//       {
//         "namespace": "openshift-machine-api",
//         "rules": [
//           {
//             "verbs": ["get", "list"],
//             "apiGroups": ["machine.openshift.io"],
//             "resources": ["machines"],
//             "resourceNames": ["worker-abc"]
//           }
//         ]
//       }
//     ]
//   }
// }

// instanceId is {name}--{uuid} where name is from the body "name" field.
var instanceId = body.name + "--" + random.uuid();
var compositeKey = clusterId + ":" + instanceId;

actionStore.save(compositeKey, body);

var now = new Date();
var expiryDate = new Date(now.getTime() + (24 * 60 * 60 * 1000));

// proxyURI /backplane/trustedaction/{cluster}/{name}--{uuid}/api/v1/namespaces/foo/pods.
var trustedAction = {
  proxyUri: "/backplane/trustedaction/" + clusterId + "/" + instanceId + "/",
  instanceId: instanceId,
  expiry: expiryDate.toISOString()
};

respond()
  .withStatusCode(200)
  .withContent(JSON.stringify(trustedAction))
  .withHeader("Content-Type", "application/json");
