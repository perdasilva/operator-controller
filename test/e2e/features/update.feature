Feature: Update ClusterExtension

  As an OLM user I would like to update a ClusterExtension from a catalog
  or get an appropriate information in case of an error.

  Background:
    Given OLM is available
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | alpha   |          | CRD, Deployment, ConfigMap |
      | test    | 1.0.0   | beta    |          |                            |
      | test    | 1.0.1   | beta    | 1.0.0    | CRD, Deployment, ConfigMap |
      | test    | 1.0.2   | alpha   | 1.0.0    | BadImage                   |
      | test    | 1.0.4   | beta    |          | CRD, Deployment, ConfigMap |
      | test    | 1.2.0   | beta    | 1.0.1    | CRD, Deployment, ConfigMap |
    And ServiceAccount "olm-sa" with needed permissions is available in test namespace

  Scenario: Update to a successor version
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension version is updated to "1.0.1"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.1" is installed in version "1.0.1"

  Scenario: Cannot update extension to non successor version
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.2.0
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message:
      """
      error upgrading from currently installed version "1.0.0": no bundles found for package "${PACKAGE:test}" matching version "1.2.0"
      """

  Scenario: Force update to non successor version
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.2.0
            upgradeConstraintPolicy: SelfCertified
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"

  @catalog-updates
  Scenario: Auto update when new version becomes available in the new catalog image ref
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
    And catalog "test" version "v2" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.3.0   | beta    | 1.2.0    | CRD, Deployment, ConfigMap |
    When catalog "test" is updated to version "v2"
    Then bundle "${PACKAGE:test}.1.3.0" is installed in version "1.3.0"

  Scenario: Auto update when new version becomes available in the same catalog image ref
    Given catalog "test" image version "v1" is also tagged as "latest"
    And catalog "test" is updated to version "latest"
    And ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
    And catalog "test" version "v2" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.3.0   | beta    | 1.2.0    | CRD, Deployment, ConfigMap |
    When catalog "test" image version "v2" is also tagged as "latest"
    Then bundle "${PACKAGE:test}.1.3.0" is installed in version "1.3.0"

  @BoxcutterRuntime
  Scenario: Update to a version with identical bundle content creates a new revision
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
            upgradeConstraintPolicy: SelfCertified
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.0" is installed in version "1.0.0"
    When ClusterExtension version is updated to "1.0.4"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.4" is installed in version "1.0.4"

  @BoxcutterRuntime
  Scenario: Detect collision when a second ClusterExtension installs the same package after an upgrade
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension version is updated to "1.0.1"
    Then ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "${PACKAGE:test}.1.0.1" is installed in version "1.0.1"
    And the current ClusterExtension is tracked for cleanup
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}-dup
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.1
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      revision object collisions
      """

  @BoxcutterRuntime
  Scenario: Each update creates a new revision and resources not present in the new revision are removed from the cluster
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
            upgradeConstraintPolicy: SelfCertified
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension version is updated to "1.2.0"
    Then bundle "${PACKAGE:test}.1.2.0" is installed in version "1.2.0"
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterExtension reports "${NAME}-2" as active revision
    And ClusterObjectSet "${NAME}-2" reports Progressing as True with Reason Succeeded
    And ClusterObjectSet "${NAME}-2" reports Available as True with Reason ProbesSucceeded
    And ClusterObjectSet "${NAME}-1" is archived
    And ClusterObjectSet "${NAME}-1" phase objects are not found or not owned by the revision

  @BoxcutterRuntime
  Scenario: Report all active revisions on ClusterExtension
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
            version: 1.0.0
            upgradeConstraintPolicy: SelfCertified
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    When ClusterExtension version is updated to "1.0.2"
    Then ClusterExtension reports "${NAME}-1, ${NAME}-2" as active revisions
    And ClusterObjectSet "${NAME}-2" reports Progressing as True with Reason RollingOut
    And ClusterObjectSet "${NAME}-2" reports Available as False with Reason ProbeFailure

  @BoxcutterRuntime
  @DeploymentConfig
  Scenario: A conflicting ClusterExtension with a higher revision does not take over resources from the original owner
    Given ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            version: 1.0.0
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And ClusterExtension "${NAME}" has 1 ClusterObjectSet
    And the current ClusterExtension is tracked for cleanup
    # Apply a second ClusterExtension pointing to the same package and version
    When ClusterExtension is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}-dup
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            version: 1.0.0
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      revision object collisions
      """
    # Verify the original ClusterExtension remains installed and unaffected
    And ClusterExtension "ce-${SCENARIO_ID}" reports Installed as True
    # Update the conflicting ClusterExtension with a deployment config env var. At the time
    # of writing, there is no other way to get the operator controller to generate another
    # ClusterObjectSet by simply targeting another bundle version in the ClusterExtension.
    # Since nothing has been installed yet for this CE, the resolver returns the currently
    # installed bundle. Injecting an environment variable via DeploymentConfig changes the
    # desired state, which causes the controller to create a second ClusterObjectSet
    # (revision 2) after the first one collides.
    When ClusterExtension is updated
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        serviceAccount:
          name: olm-sa
        config:
          configType: Inline
          inline:
            deploymentConfig:
              env:
                - name: MY_VAR
                  value: "my-value"
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            version: 1.0.0
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": ${CATALOG:test}
      """
    Then ClusterExtension "${NAME}" has 2 ClusterObjectSets
    # The higher-revision COS (revision 2) should also collide, not take over resources
    And ClusterExtension reports Progressing as True with Reason Retrying and Message includes:
      """
      revision object collisions
      """
    # Verify the original ClusterExtension is still installed after the higher-revision collision
    And ClusterExtension "ce-${SCENARIO_ID}" reports Installed as True
