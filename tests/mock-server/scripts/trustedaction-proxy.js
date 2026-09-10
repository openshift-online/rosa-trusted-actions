// Catch-all Kubernetes proxy for a trusted action instance.
//
// The resource this runs behind declares no method and ends in a wildcard, so every HTTP verb and
// any number of trailing segments land here. The Kubernetes suffix is recovered from the full
// request path rather than from a path parameter.

// {clusterId}:{instanceId} -> the granted request body, written by the create script
var actionStore = stores.open('actions');
// {clusterId}:{kubernetes path} -> the preloaded fixture entry for that resource
var clusterStore = stores.open('cluster');

var clusterId = context.request.pathParams.clusterId;
var instanceId = context.request.pathParams.trustedActionInstanceId;

function jsonResponse(statusCode, body) {
  respond()
    .withStatusCode(statusCode)
    .withContent(JSON.stringify(body))
    .withHeader("Content-Type", "application/json");
}

// the contract's Error object
function errorResponse(statusCode, message) {
  jsonResponse(statusCode, { statusCode: statusCode, message: message });
}

// a metav1.Status, so a Kubernetes client decodes the not-found rather than choking on it
function notFoundResponse(message) {
  jsonResponse(404, {
    apiVersion: "v1",
    kind: "Status",
    metadata: {},
    status: "Failure",
    message: message,
    reason: "NotFound",
    code: 404
  });
}

function has(object, key) {
  return Object.prototype.hasOwnProperty.call(object, key);
}

// The Kubernetes verb depends on both the method and whether the path addresses a collection or an
// individual resource, so that listing and getting are scoped separately.
function kubeVerb(method, isCollection) {
  switch (method) {
    case "GET":
      return isCollection ? "list" : "get";
    case "POST":
      return "create";
    case "PUT":
      return "update";
    case "PATCH":
      return "patch";
    case "DELETE":
      return isCollection ? "deletecollection" : "delete";
    default:
      return null;
  }
}

// Exact matching only at this stage; wildcards and resource names are ticket 04.
function ruleMatches(rule, verb, identity) {
  return (rule.verbs || []).indexOf(verb) >= 0 &&
    (rule.apiGroups || []).indexOf(identity.apiGroup) >= 0 &&
    (rule.resources || []).indexOf(identity.resource) >= 0;
}

// The granted rules are the only rule set whose correctness matters: the entry's identity block is
// what the resource is, not a ceiling. So the single denial reason is that the caller never
// requested the permission.
function isPermitted(rbac, verb, identity) {
  if (!rbac) {
    return false;
  }

  var clusterRoleRules = rbac.clusterRoleRules || [];
  for (var i = 0; i < clusterRoleRules.length; i++) {
    if (ruleMatches(clusterRoleRules[i], verb, identity)) {
      return true;
    }
  }

  var roles = rbac.roles || [];
  for (var r = 0; r < roles.length; r++) {
    if (roles[r].namespace !== identity.namespace) {
      continue;
    }
    var rules = roles[r].rules || [];
    for (var j = 0; j < rules.length; j++) {
      if (ruleMatches(rules[j], verb, identity)) {
        return true;
      }
    }
  }

  return false;
}

// The fixture writes the object; the entry's declared type fills in only what it omitted, so what
// is served is always decodable into a typed object. Copied rather than filled in place: the store
// may hand back a reference other requests share.
function withDeclaredType(object, entry) {
  var typed = {};
  for (var key in object) {
    if (has(object, key)) {
      typed[key] = object[key];
    }
  }
  if (!typed.apiVersion) {
    typed.apiVersion = entry.apiVersion;
  }
  if (!typed.kind) {
    typed.kind = entry.kind;
  }
  return typed;
}

// The request path is the proxy prefix followed by the Kubernetes path. Appending an absolute
// Kubernetes path to proxyUri is the documented usage, but a client that joins them itself may
// leave a doubled slash, so collapse it.
var requestPath = String(context.request.path);
var queryStart = requestPath.indexOf("?");
if (queryStart >= 0) {
  requestPath = requestPath.substring(0, queryStart);
}

var prefix = "/backplane/trustedaction/" + clusterId + "/" + instanceId;
var kubePath = requestPath.substring(prefix.length).replace(/^\/+/, "/");

var granted = actionStore.load(clusterId + ":" + instanceId);
var entry = clusterStore.load(clusterId + ":" + kubePath);
var fixtureKey = clusterId + ":" + kubePath;

if (!granted) {
  // covers both an unknown instance and one belonging to a different cluster: the key is composite,
  // so an instance created against another cluster simply is not there
  errorResponse(404, "trusted action instance not found for this cluster: " + instanceId);
} else if (!entry) {
  // a gap in the mock must not be able to masquerade as a legitimate not-found
  errorResponse(501, "no fixture entry for " + fixtureKey);
} else if (!has(entry, "items") && !has(entry, "object")) {
  // neither shape, so the entry cannot say what it holds -- another gap, not a not-found
  errorResponse(501, "fixture entry " + fixtureKey + " declares neither items nor object");
} else {
  var isCollection = has(entry, "items");
  var verb = kubeVerb(String(context.request.method).toUpperCase(), isCollection);
  var identity = entry.identity || {};

  if (verb === null) {
    errorResponse(501, "no Kubernetes verb for HTTP method " + context.request.method);
  } else if (!isPermitted(granted.rbac, verb, identity)) {
    errorResponse(403, "the trusted action did not request " + verb + " on " +
      (identity.apiGroup ? identity.apiGroup + "/" : "") + identity.resource +
      " in namespace " + identity.namespace);
  } else if (verb !== "list" && verb !== "get") {
    // permitted, but the fixture is a read model: say so rather than answer a write with content
    errorResponse(501, "the mock does not implement " + verb);
  } else if (isCollection) {
    jsonResponse(200, {
      apiVersion: entry.apiVersion,
      kind: entry.kind + "List",
      metadata: { resourceVersion: "1" },
      items: entry.items
    });
  } else if (entry.object === null) {
    notFoundResponse(kubePath + " not found");
  } else {
    jsonResponse(200, withDeclaredType(entry.object, entry));
  }
}
