Feature: Uninstall ClusterExtension

  As an OLM user I would like to uninstall a cluster extension.

  Background:
    Given OLM is available
    And ClusterCatalog "test" serves bundles
    And ServiceAccount "olm-sa" with needed permissions is available in ${TEST_NAMESPACE}
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
            packageName: test
            selector:
              matchLabels:
                "olm.operatorframework.io/metadata.name": test-catalog
      """
    And ClusterExtension is rolled out
    And ClusterExtension is available
    And bundle "test-operator.1.2.0" is installed in version "1.2.0"
  Scenario:  Uninstall latest available version
    When ClusterExtension "${NAME}" is deleted
    Then resource "networkpolicy/test-operator-network-policy" is removed
    And resource "configmap/test-configmap" is removed
    And resource "deployment/test-operator" is removed
    And resource "clusterrole/testoperator.v1.2.-37mym6pni2xxmai9n7fmhtbn9i348lx7o619rmf3ypio" is removed
    And resource "clusterrole/testoperator.v1.2.0-t88i5epjh8oxp4klplhjyrsekwcp92b27w03ayr1ku5" is removed
    And resource "clusterrolebinding/testoperator.v1.2.-37mym6pni2xxmai9n7fmhtbn9i348lx7o619rmf3ypio" is removed
    And resource "clusterrolebinding/testoperator.v1.2.0-t88i5epjh8oxp4klplhjyrsekwcp92b27w03ayr1ku5" is removed
