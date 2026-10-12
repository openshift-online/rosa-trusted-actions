var actionStore = stores.open('actions');

var clusterId = context.request.pathParams.clusterId;
var instanceId = context.request.pathParams.trustedActionInstanceId;

var compositeKey = clusterId + ":" + instanceId;

// the engine executes this script at top level, so branch rather than return early
if (!actionStore.load(compositeKey)) {
  respond()
    .withStatusCode(404)
    .withContent(JSON.stringify({ statusCode: 404, message: "trusted action instance not found: " + instanceId }))
    .withHeader("Content-Type", "application/json");
} else {
  actionStore.delete(compositeKey);

  // contract: 200 with Content-Type application/json and a JSON string body
  respond()
    .withStatusCode(200)
    .withContent(JSON.stringify("Trusted action resources deleted"))
    .withHeader("Content-Type", "application/json");
}
