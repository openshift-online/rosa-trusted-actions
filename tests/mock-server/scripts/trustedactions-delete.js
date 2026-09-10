var actionStore = stores.open('actions');

var clusterId = context.request.pathParams.clusterId;
var instanceId = context.request.pathParams.trustedActionInstanceId;

var compositeKey = clusterId + ":" + instanceId;

// the engine executes this script at top level, so branch rather than return early
if (!actionStore.load(compositeKey)) {
  respond()
    .withStatusCode(404)
    .withContent("ClusterId or trustedActionId not found in store")
    .withHeader("Content-Type", "text/plain");
} else {
  actionStore.delete(compositeKey);

  respond()
    .withStatusCode(200)
    .withContent("Deleted clusterId and trustedActionId from store")
    .withHeader("Content-Type", "text/plain");
}
