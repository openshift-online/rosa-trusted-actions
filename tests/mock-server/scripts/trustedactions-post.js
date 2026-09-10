// keyed {clusterId}:{instanceId}, holding the granted request body for the proxy to read
var actionStore = stores.open('actions');

var clusterId = context.request.pathParams.clusterId;

// the request body is required by the contract, so an absent or unparseable one is an
// error to report rather than something to throw on
var body = null;
if (context.request.body) {
  try {
    body = JSON.parse(context.request.body);
  } catch (e) {
    body = null;
  }
}

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

// CreateTrustedActionRequest requires name, customerDataAccess and rbac; the nested
// TrustedActionRbacDecl requires clusterRoleRules and roles.
var rejection = null;

if (body === null || typeof body !== "object") {
  rejection = "request body must be a JSON object";
} else {
  var missing = [];

  if (body.name === undefined || body.name === null) {
    missing.push("name");
  }
  if (body.customerDataAccess === undefined || body.customerDataAccess === null) {
    missing.push("customerDataAccess");
  }
  if (body.rbac === undefined || body.rbac === null) {
    missing.push("rbac");
  } else {
    if (body.rbac.clusterRoleRules === undefined || body.rbac.clusterRoleRules === null) {
      missing.push("rbac.clusterRoleRules");
    }
    if (body.rbac.roles === undefined || body.rbac.roles === null) {
      missing.push("rbac.roles");
    }
  }

  if (missing.length > 0) {
    rejection = "missing required field(s): " + missing.join(", ");
  }
}

// the engine executes this script at top level, so branch rather than return early
if (rejection !== null) {
  respond()
    .withStatusCode(400)
    .withContent(JSON.stringify({ statusCode: 400, message: rejection }))
    .withHeader("Content-Type", "application/json");
} else {
  // instanceId is {name}--{uuid} where name is from the body "name" field.
  var instanceId = body.name + "--" + random.uuid();
  var compositeKey = clusterId + ":" + instanceId;

  actionStore.save(compositeKey, body);

  var now = new Date();
  var expiryDate = new Date(now.getTime() + (24 * 60 * 60 * 1000));

  // proxyURI /backplane/trustedaction/{cluster}/{name}--{uuid}, with the Kubernetes path appended:
  // /backplane/trustedaction/{cluster}/{name}--{uuid}/api/v1/namespaces/foo/pods. No trailing slash,
  // so appending an absolute Kubernetes path does not produce a doubled one the router redirects.
  var trustedAction = {
    proxyUri: "/backplane/trustedaction/" + clusterId + "/" + instanceId,
    instanceId: instanceId,
    expiry: expiryDate.toISOString()
  };

  respond()
    .withStatusCode(200)
    .withContent(JSON.stringify(trustedAction))
    .withHeader("Content-Type", "application/json");
}
