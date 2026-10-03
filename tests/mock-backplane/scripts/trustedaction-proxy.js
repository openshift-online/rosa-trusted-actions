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

// Kubernetes treats "*" in a rule's verbs, apiGroups or resources as matching anything, and the
// core API group as the empty string on both sides of the comparison.
function listMatches(values, wanted) {
  var list = values || [];
  for (var i = 0; i < list.length; i++) {
    if (list[i] === "*" || list[i] === wanted) {
      return true;
    }
  }
  return false;
}

// resourceNames narrows a rule to individually named objects, so a rule carrying them cannot cover
// a request that names none. That is what stops a grant on one object from listing its siblings,
// and what stops it from reading them by name.
function nameMatches(rule, name) {
  var names = rule.resourceNames || [];
  if (names.length === 0) {
    return true;
  }
  return name !== "" && names.indexOf(name) >= 0;
}

function ruleMatches(rule, verb, target) {
  return listMatches(rule.verbs, verb) &&
    listMatches(rule.apiGroups, target.apiGroup) &&
    listMatches(rule.resources, target.resource) &&
    nameMatches(rule, target.name);
}

// The granted rules are the only rule set whose correctness matters: the entry's identity block is
// what the resource is, not a ceiling. So the single denial reason is that the caller never
// requested the permission.
function isPermitted(rbac, verb, target) {
  if (!rbac) {
    return false;
  }

  var clusterRoleRules = rbac.clusterRoleRules || [];
  for (var i = 0; i < clusterRoleRules.length; i++) {
    if (ruleMatches(clusterRoleRules[i], verb, target)) {
      return true;
    }
  }

  // A Role is bound in one namespace, so it authorises neither a cluster-scoped resource nor a
  // collection read across all namespaces -- both of which carry no namespace at all.
  if (target.namespace === "") {
    return false;
  }

  var roles = rbac.roles || [];
  for (var r = 0; r < roles.length; r++) {
    if (roles[r].namespace !== target.namespace) {
      continue;
    }
    var rules = roles[r].rules || [];
    for (var j = 0; j < rules.length; j++) {
      if (ruleMatches(rules[j], verb, target)) {
        return true;
      }
    }
  }

  return false;
}

// The entry's identity says what the resource is; the path says which object of it was addressed.
// Only the name, the subresource and resource-ness are read from the path -- enough to honour
// resourceNames and to be honest about the two surfaces the mock does not model, without
// reimplementing the API server's path parsing.
function parseKubePath(path) {
  var segments = [];
  var raw = path.split("/");
  for (var i = 0; i < raw.length; i++) {
    if (raw[i] !== "") {
      segments.push(raw[i]);
    }
  }

  // /api/{version}/... is the core group; /apis/{group}/{version}/... is every other one. Anything
  // else -- /healthz, /version, /openapi/v2 -- addresses no resource, and so does a bare
  // group-version, which is discovery.
  var rest;
  if (segments[0] === "api" && segments.length > 2) {
    rest = segments.slice(2);
  } else if (segments[0] === "apis" && segments.length > 3) {
    rest = segments.slice(3);
  } else {
    return { nonResource: true };
  }

  // namespaces/{ns}/... is a namespaced request; without the trailing resource it is the
  // namespaces resource itself, which is cluster-scoped. The API server resolves the same
  // ambiguity by hardcoding the namespace's own two subresources, so hardcode them too.
  if (rest[0] === "namespaces" && rest.length > 2 &&
      rest[2] !== "status" && rest[2] !== "finalize") {
    rest = rest.slice(2);
  }

  return {
    nonResource: false,
    resource: rest[0],
    name: rest.length > 1 ? rest[1] : "",
    subresource: rest.length > 2 ? rest.slice(2).join("/") : ""
  };
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

// Selectors are not applied, so say so on the way past: an unfiltered result must not be mistaken
// for a filtered one.
var selectorParams = ["labelSelector", "fieldSelector"];
var queryParams = context.request.queryParams || {};
for (var s = 0; s < selectorParams.length; s++) {
  if (has(queryParams, selectorParams[s])) {
    console.warn("ignoring " + selectorParams[s] + "=" + queryParams[selectorParams[s]] +
      " on " + kubePath + ": the mock does not filter, the result is unfiltered");
  }
}

var request = parseKubePath(kubePath);

var granted = actionStore.load(clusterId + ":" + instanceId);
var entry = clusterStore.load(clusterId + ":" + kubePath);
var fixtureKey = clusterId + ":" + kubePath;

if (!granted) {
  // covers both an unknown instance and one belonging to a different cluster: the key is composite,
  // so an instance created against another cluster simply is not there
  errorResponse(404, "trusted action instance not found for this cluster: " + instanceId);
} else if (request.nonResource) {
  // reported before the fixture lookup, so the gap is named for what it is rather than as a
  // missing entry a fixture author could try to add
  errorResponse(501, "the mock does not implement non-resource URL requests: " + kubePath);
} else if (request.subresource !== "") {
  errorResponse(501, "the mock does not implement subresource requests: " +
    request.resource + "/" + request.subresource);
} else if (!entry) {
  // a gap in the mock must not be able to masquerade as a legitimate not-found
  errorResponse(501, "no fixture entry for " + fixtureKey);
} else if (!has(entry, "items") && !has(entry, "object")) {
  // neither shape, so the entry cannot say what it holds -- another gap, not a not-found
  errorResponse(501, "fixture entry " + fixtureKey + " declares neither items nor object");
} else {
  var isCollection = has(entry, "items");
  var verb = kubeVerb(String(context.request.method).toUpperCase(), isCollection);
  // What access is decided against: the entry says what the resource is, the path says which
  // object of it was addressed. An omitted apiGroup is the core group and an omitted namespace is
  // cluster scope, so normalise both to the empty string the matcher compares against.
  var declared = entry.identity || {};
  var target = {
    apiGroup: declared.apiGroup || "",
    resource: declared.resource,
    namespace: declared.namespace || "",
    name: request.name
  };
  var scope = target.namespace === "" ? "at cluster scope" : "in namespace " + target.namespace;

  if (verb === null) {
    errorResponse(501, "no Kubernetes verb for HTTP method " + context.request.method);
  } else if (!isPermitted(granted.rbac, verb, target)) {
    errorResponse(403, "the trusted action did not request " + verb + " on " +
      (target.apiGroup ? target.apiGroup + "/" : "") + target.resource +
      (target.name ? "/" + target.name : "") + " " + scope);
  } else if (verb === "create") {
    // Echo the request body back as the created object; real K8s would return 201.
    var created = {};
    try { created = JSON.parse(String(context.request.body)); } catch (e) {}
    if (!created.apiVersion) { created.apiVersion = entry.apiVersion; }
    var singularKind = entry.kind ? entry.kind.replace(/List$/, "") : "Unknown";
    if (!created.kind) { created.kind = singularKind; }
    respond()
      .withStatusCode(201)
      .withContent(JSON.stringify(created))
      .withHeader("Content-Type", "application/json");
  } else if (verb === "patch" || verb === "update") {
    // Return the stored fixture object; the mock does not apply the diff.
    var stored = entry.object ? withDeclaredType(entry.object, entry) : { apiVersion: entry.apiVersion, kind: entry.kind };
    jsonResponse(200, stored);
  } else if (verb === "delete" || verb === "deletecollection") {
    jsonResponse(200, { apiVersion: "v1", kind: "Status", metadata: {}, status: "Success", code: 200 });
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
