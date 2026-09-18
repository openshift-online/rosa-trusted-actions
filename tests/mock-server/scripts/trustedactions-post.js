// keyed {clusterId}:{instanceId}, holding the granted request body for the proxy to read
var actionStore = stores.open('actions');
// keyed {clusterId}:_flags, holding per-cluster configuration flags
var clusterStore = stores.open('cluster');

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

// Checks whether a single policy rule permits any forbidden combination.
// Verb and resource matching is case-insensitive; API group matching is exact.
function ruleGrantsForbidden(rule, policy) {
  var verbs = rule.verbs || [];
  var apiGroups = rule.apiGroups || [];
  var resources = rule.resources || [];

  var apiGroupHit = false;
  for (var a = 0; a < apiGroups.length; a++) {
    if (apiGroups[a] === "*" || apiGroups[a] === policy.apiGroup) {
      apiGroupHit = true;
      break;
    }
  }
  if (!apiGroupHit) return false;

  var resourceHit = false;
  for (var r = 0; r < resources.length; r++) {
    var res = resources[r].toLowerCase();
    if (res === "*") {
      resourceHit = true;
      break;
    }
    for (var pr = 0; pr < policy.resources.length; pr++) {
      if (res === policy.resources[pr]) {
        resourceHit = true;
        break;
      }
    }
    if (resourceHit) break;
  }
  if (!resourceHit) return false;

  for (var v = 0; v < verbs.length; v++) {
    var verb = verbs[v].toLowerCase();
    if (verb === "*") return true;
    for (var pv = 0; pv < policy.verbs.length; pv++) {
      if (verb === policy.verbs[pv]) return true;
    }
  }
  return false;
}

// Forbidden combinations that mirror backplane's validation policy.
// Note: this is a copy of backplane's policy and will drift silently if backplane changes it.
var FORBIDDEN_POLICIES = [
  {
    apiGroup: "",
    resources: ["secrets"],
    verbs: ["get", "list", "watch", "patch"],
    description: "reads and patches of secrets in the core API group"
  },
  {
    apiGroup: "work.open-cluster-management.io",
    resources: ["manifestworks"],
    verbs: ["get", "patch", "delete", "deletecollection"],
    description: "gets, patches and deletes of manifestworks in work.open-cluster-management.io"
  }
];

// Returns a rejection message if any rule in the RBAC grants a forbidden combination,
// or null if all rules are permitted.
function checkForbiddenRules(rbac) {
  if (!rbac) return null;

  var allRules = [];
  var clusterRoleRules = rbac.clusterRoleRules || [];
  for (var i = 0; i < clusterRoleRules.length; i++) {
    allRules.push(clusterRoleRules[i]);
  }
  var roles = rbac.roles || [];
  for (var r = 0; r < roles.length; r++) {
    var rules = (roles[r] && roles[r].rules) || [];
    for (var j = 0; j < rules.length; j++) {
      allRules.push(rules[j]);
    }
  }

  for (var k = 0; k < allRules.length; k++) {
    for (var p = 0; p < FORBIDDEN_POLICIES.length; p++) {
      if (ruleGrantsForbidden(allRules[k], FORBIDDEN_POLICIES[p])) {
        return "rule grants " + FORBIDDEN_POLICIES[p].description + ", which backplane forbids";
      }
    }
  }
  return null;
}

var rejectionStatus = null;
var rejectionMessage = null;

// Validate required fields
if (body === null || typeof body !== "object") {
  rejectionStatus = 400;
  rejectionMessage = "request body must be a JSON object";
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
    rejectionStatus = 400;
    rejectionMessage = "missing required field(s): " + missing.join(", ");
  }
}

// Check reserved infra RBAC fields: explicit null is tolerated, a populated object is not
if (rejectionStatus === null) {
  var infraFields = ["managementClusterRbac", "serviceClusterRbac", "hiveClusterRbac"];
  for (var i = 0; i < infraFields.length; i++) {
    var field = infraFields[i];
    if (body[field] !== undefined && body[field] !== null) {
      rejectionStatus = 400;
      rejectionMessage = field + " is reserved and not yet implemented for trusted actions; omit it or send null";
      break;
    }
  }
}

// Check for known cluster: the fixture must carry a _flags entry for this cluster
if (rejectionStatus === null) {
  var flags = clusterStore.load(clusterId + ":_flags");
  if (flags === null || flags === undefined) {
    rejectionStatus = 400;
    rejectionMessage = "no fixture entries for cluster: " + clusterId;
  }
}

// Apply backplane's forbidden-rule policy
if (rejectionStatus === null) {
  var forbiddenReason = checkForbiddenRules(body.rbac);
  if (forbiddenReason !== null) {
    rejectionStatus = 400;
    rejectionMessage = forbiddenReason;
  }
}

// Fixture flags let callers trigger declared error statuses that have no locally computable truth
if (rejectionStatus === null && flags) {
  var overrideStatus = flags.createStatus;
  if (overrideStatus && overrideStatus !== 200) {
    rejectionStatus = overrideStatus;
    rejectionMessage = flags.createMessage || ("fixture flag set createStatus to " + overrideStatus);
  }
}

// the engine executes this script at top level, so branch rather than return early
if (rejectionStatus !== null) {
  respond()
    .withStatusCode(rejectionStatus)
    .withContent(JSON.stringify({ statusCode: rejectionStatus, message: rejectionMessage }))
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
