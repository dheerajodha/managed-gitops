package db

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

// isEmptyValues returns an error if at least one of the parameters is nil or empty.
// The returned error string indicates which parameter was empty, plus the calling function.
//
// This function can be used as a generic check of empty or nil values, in order to reduce
// the amount of boilerplate code.
//
// See functions that are calling this one for examples.
func isEmptyValues(callLocation string, params ...any) error {

	if len(params)%2 == 1 {
		return fmt.Errorf("invalid number of parameters, expected an even number: %v", len(params))
	}

	if len(params) == 0 {
		return fmt.Errorf("invalid number of parameters, at least 2 expected")
	}

	x := 0
	for {

		fieldNameParam := params[x]

		if fieldNameParam == nil || fieldNameParam == "" {
			return fmt.Errorf("field name in position %d was empty, in %v", x, callLocation)
		}

		fieldName, isString := fieldNameParam.(string)
		if !isString {
			return fmt.Errorf("field name in position %d is not a string, in %v", x, callLocation)
		}

		value := params[x+1]
		if value == nil {
			return fmt.Errorf("%v field should not be nil, in %v", fieldName, callLocation)

		} else if valueStr, isString := value.(string); isString && len(strings.TrimSpace(valueStr)) == 0 {
			return fmt.Errorf("%v field should not be empty string, in %v", fieldName, callLocation)
		}

		x += 2
		if x >= len(params) {
			break
		}
	}

	return nil

}

// validateQueryParams is common, simple validation logic shared by most entities
func validateQueryParams(entityId string, dbq *PostgreSQLDatabaseQueries) error {
	if dbq.dbConnection == nil {
		return fmt.Errorf("database connection is nil")
	}

	if IsEmpty(entityId) {
		debug.PrintStack()
		return fmt.Errorf("primary key is empty")
	}

	return nil
}

// validateUnsafeQueryParams is common, simple validation logic shared by most entities
func validateUnsafeQueryParams(entityId string, dbq *PostgreSQLDatabaseQueries) error {

	if err := validateQueryParams(entityId, dbq); err != nil {
		return err
	}

	if !dbq.allowUnsafe {
		return fmt.Errorf("unsafe operation is not allowed in this context")
	}

	return nil
}

// validateQueryParams is common, simple validation logic shared by most entities
func validateQueryParamsEntity(entity any, dbq *PostgreSQLDatabaseQueries) error {
	if dbq.dbConnection == nil {
		return fmt.Errorf("database connection is nil")
	}

	if entity == nil {
		return fmt.Errorf("query parameter value is nil")
	}

	return nil
}

// validateGenericEntity is common, simple validation logic shared by most entities
func validateUnsafeQueryParamsNoPK(dbq *PostgreSQLDatabaseQueries) error {

	if dbq.dbConnection == nil {
		return fmt.Errorf("database connection is nil")
	}

	if !dbq.allowUnsafe {
		return fmt.Errorf("unsafe operation is not allowed in this context")
	}

	return nil
}

// validateQueryParams is common, simple validation logic shared by most entities
func validateQueryParamsNoPK(dbq *PostgreSQLDatabaseQueries) error {
	if dbq.dbConnection == nil {
		return fmt.Errorf("database connection is nil")
	}

	return nil
}

func (e *APICRToDatabaseMapping) ShortString() string {

	res := ""
	res += "name: " + e.APIResourceName + ", "
	res += "namespace: " + e.APIResourceNamespace + ", "
	res += "resource-type: : " + string(e.APIResourceType) + ", "
	res += "namespace-uid: " + e.NamespaceUID + ", "
	res += "db-relation-key: " + e.DBRelationKey + ", "
	res += "db-relation-type: " + string(e.DBRelationType)

	return res
}

func (o *Operation) ShortString() string {
	res := ""
	res += "operation-id: " + o.Operation_id + ", "
	res += "instance-id: " + o.Instance_id + ", "
	res += "owner: " + o.Operation_owner_user_id + ", "
	res += "resource: " + o.Resource_id + ", "
	res += "resource-type: " + string(o.Resource_type) + ", "
	return res
}

func (o *Operation) LongString() string {
	res := ""
	res += "instance-id: " + o.Instance_id + ", "
	res += "operation-id: " + o.Operation_id + ", "
	res += "owner: " + o.Operation_owner_user_id + ", "
	res += "resource: " + o.Resource_id + ", "
	res += "resource-type: " + string(o.Resource_type) + ", "

	res += "human-readable-state: " + o.Human_readable_state + ", "
	res += "state: " + string(o.State) + ", "
	res += fmt.Sprintf("last-status-update: %v", o.Last_state_update) + ", "
	res += fmt.Sprintf("created_on: %v", o.Last_state_update)

	return res
}

func generateUuid() string {
	return uuid.New().String()
}

func IsEmpty(str string) bool {
	return len(strings.TrimSpace(str)) == 0
}

func ConvertSnakeCaseToCamelCase(fieldName string) string {
	splitFieldName := strings.Split(fieldName, "_")
	var fieldNameInCamelCase string

	for i := 0; i < len(splitFieldName); i++ {
		if splitFieldName[i] == "id" || splitFieldName[i] == "uid" || splitFieldName[i] == "url" {
			fieldNameInCamelCase += strings.ToUpper(splitFieldName[i])
		} else {
			// TODO: strings.Title() has been deprecated in Go 1.18. Use golang.org/x/text/cases instead.
			fieldNameInCamelCase += strings.Title(splitFieldName[i]) // nolint:staticcheck
		}
	}

	return fieldNameInCamelCase
}

// A generic function to validate length of string values in input provided by users.
// The max length of string is checked using constant variables defined for each type and field in db_field_constants.go
func validateFieldLength(obj any) error {
	valuesOfObject := reflect.ValueOf(obj).Elem()
	typeOfObject := reflect.TypeOf(obj).Elem().Name()

	// Iterate through each field present in object
	for i := 0; i < valuesOfObject.NumField(); i++ {
		fieldName := valuesOfObject.Type().Field(i).Name
		fieldValue := valuesOfObject.FieldByName(fieldName)
		fieldType := fieldValue.Type().Name()

		if fieldType != "string" {
			continue
		}
		// Format object type and field name according to constants defined in db_field_constants.go
		maximumSize := getConstantValue(ConvertSnakeCaseToCamelCase(typeOfObject + "_" + fieldName + "_Length"))

		if len(fieldValue.String()) > maximumSize {
			return fmt.Errorf("%v value exceeds maximum size: max: %d, actual: %d", fieldName, maximumSize, len(fieldValue.String()))
		}
	}
	return nil
}

func IsMaxLengthError(err error) bool {
	if err != nil {
		return strings.Contains(err.Error(), "value exceeds maximum size")
	}
	return false
}

var testClusterUser = &ClusterUser{
	Clusteruser_id: "test-user",
	User_name:      "test-user",
}

func CreateSampleData(dbq AllDatabaseQueries) (*ClusterCredentials, *ManagedEnvironment, *GitopsEngineCluster, *GitopsEngineInstance, *ClusterAccess, error) {

	ctx := context.Background()
	var err error

	clusterCredentials, managedEnvironment, engineCluster, engineInstance, clusterAccess := generateSampleData()

	if err = dbq.CreateClusterCredentials(ctx, &clusterCredentials); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	if err = dbq.CreateManagedEnvironment(ctx, &managedEnvironment); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	if err = dbq.CreateGitopsEngineCluster(ctx, &engineCluster); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	if err = dbq.CreateGitopsEngineInstance(ctx, &engineInstance); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	if err = dbq.CreateClusterAccess(ctx, &clusterAccess); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	return &clusterCredentials, &managedEnvironment, &engineCluster, &engineInstance, &clusterAccess, nil

}

func generateSampleData() (ClusterCredentials, ManagedEnvironment, GitopsEngineCluster, GitopsEngineInstance, ClusterAccess) {
	clusterCredentials := ClusterCredentials{
		Clustercredentials_cred_id:  "test-cluster-creds-test",
		Host:                        "host",
		Kube_config:                 "kube-config",
		Kube_config_context:         "kube-config-context",
		Serviceaccount_bearer_token: "serviceaccount_bearer_token",
		Serviceaccount_ns:           "Serviceaccount_ns",
	}

	managedEnvironment := ManagedEnvironment{
		Managedenvironment_id: "test-managed-env-914",
		Clustercredentials_id: clusterCredentials.Clustercredentials_cred_id,
		Name:                  "my env",
	}

	gitopsEngineCluster := GitopsEngineCluster{
		Gitopsenginecluster_id: "test-fake-cluster-914",
		Clustercredentials_id:  clusterCredentials.Clustercredentials_cred_id,
	}

	gitopsEngineInstance := GitopsEngineInstance{
		Gitopsengineinstance_id: "test-fake-engine-instance-id",
		Namespace_name:          "argocd",
		Namespace_uid:           "test-fake-namespace-914",
		EngineCluster_id:        gitopsEngineCluster.Gitopsenginecluster_id,
	}

	clusterAccess := ClusterAccess{
		Clusteraccess_user_id:                   testClusterUser.Clusteruser_id,
		Clusteraccess_managed_environment_id:    managedEnvironment.Managedenvironment_id,
		Clusteraccess_gitops_engine_instance_id: gitopsEngineInstance.Gitopsengineinstance_id,
	}

	return clusterCredentials, managedEnvironment, gitopsEngineCluster, gitopsEngineInstance, clusterAccess
}

// SetupForTestingDBGinkgo call this first, if you need to set up the database for tests written in Ginkgo.
func SetupForTestingDBGinkgo() error {

	ctx := context.Background()

	// 'testSetup' deletes all database rows that start with 'test-' in the primary key of the row.
	// This ensures a clean slate for the test run.

	dbq, err := NewUnsafePostgresDBQueries(true, true)
	Expect(err).ToNot(HaveOccurred())

	defer dbq.CloseDatabase()

	var applicationOwners []ApplicationOwner
	err = dbq.UnsafeListAllApplicationOwners(ctx, &applicationOwners)
	Expect(err).ToNot(HaveOccurred())

	for _, applicationOwner := range applicationOwners {
		if strings.HasPrefix(applicationOwner.ApplicationOwnerApplicationID, "test-") {

			rowsAffected, err := dbq.DeleteApplicationOwner(ctx, applicationOwner.ApplicationOwnerApplicationID)
			Expect(err).ToNot(HaveOccurred())

			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var syncOperations []SyncOperation

	err = dbq.UnsafeListAllSyncOperations(ctx, &syncOperations)
	Expect(err).ToNot(HaveOccurred())

	for _, syncOperation := range syncOperations {
		if strings.HasPrefix(syncOperation.SyncOperation_id, "test-") {
			rowsAffected, err := dbq.DeleteSyncOperationById(ctx, syncOperation.SyncOperation_id)
			Expect(err).ToNot(HaveOccurred())

			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var applicationStates []ApplicationState
	err = dbq.UnsafeListAllApplicationStates(ctx, &applicationStates)
	Expect(err).ToNot(HaveOccurred())

	for _, applicationState := range applicationStates {
		if strings.HasPrefix(applicationState.Applicationstate_application_id, "test-") {
			rowsAffected, err := dbq.DeleteApplicationStateById(ctx, applicationState.Applicationstate_application_id)
			Expect(err).ToNot(HaveOccurred())
			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	// Create a list of gitops engine instance uids that were created by test cases; we
	// will later use this to delete old Operations rows, that reference these instances.
	gitopsEngineInstanceUIDsToDelete := map[string]any{}
	{
		var engineInstances []GitopsEngineInstance
		err = dbq.UnsafeListAllGitopsEngineInstances(ctx, &engineInstances)
		Expect(err).ToNot(HaveOccurred())

		for _, gitopsEngineInstance := range engineInstances {

			if strings.HasPrefix(gitopsEngineInstance.Gitopsengineinstance_id, "test-") ||
				strings.HasPrefix(gitopsEngineInstance.Namespace_name, "test-") {

				gitopsEngineInstanceUIDsToDelete[gitopsEngineInstance.Gitopsengineinstance_id] = ""
			}
		}
	}

	var appProjectRepositories []AppProjectRepository

	err = dbq.UnsafeListAllAppProjectRepositories(ctx, &appProjectRepositories)
	Expect(err).ToNot(HaveOccurred())

	for idx := range appProjectRepositories {
		item := appProjectRepositories[idx]
		if strings.HasPrefix(item.RepositorycredentialsID, "test-") {
			rowsAffected, err := dbq.DeleteAppProjectRepositoryByRepoCredId(ctx, &item)
			Expect(err).ToNot(HaveOccurred())
			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var appProjectManagedEnvs []AppProjectManagedEnvironment

	err = dbq.UnsafeListAllAppProjectManagedEnvironments(ctx, &appProjectManagedEnvs)
	Expect(err).ToNot(HaveOccurred())

	for idx := range appProjectManagedEnvs {
		item := appProjectManagedEnvs[idx]
		if strings.HasPrefix(item.Managed_environment_id, "test-") {
			rowsAffected, err := dbq.DeleteAppProjectManagedEnvironmentByManagedEnvId(ctx, &item)
			Expect(err).ToNot(HaveOccurred())
			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var operations []Operation
	err = dbq.UnsafeListAllOperations(ctx, &operations)
	Expect(err).ToNot(HaveOccurred())

	for _, operation := range operations {

		// Clean up any operations that reference GitOpsEngineInstance that are going to be deleted below.
		_, instanceToBeDeleted := gitopsEngineInstanceUIDsToDelete[operation.Instance_id]

		if instanceToBeDeleted || strings.HasPrefix(operation.Operation_id, "test-") || strings.HasPrefix(operation.Operation_owner_user_id, "test-") {
			rowsAffected, err := dbq.CheckedDeleteOperationById(ctx, operation.Operation_id, operation.Operation_owner_user_id)
			Expect(rowsAffected).Should(Equal(1))
			Expect(err).ToNot(HaveOccurred())

		}
	}

	// Delete all RepositoryCredential database rows that start with 'test-' in the primary key of the row.
	err = removeAnyRepositoryCredentialsTestEntries(ctx, dbq)
	Expect(err).ToNot(HaveOccurred())

	var deploymentToApplicationMappings []DeploymentToApplicationMapping

	err = dbq.UnsafeListAllDeploymentToApplicationMapping(ctx, &deploymentToApplicationMappings)
	Expect(err).ToNot(HaveOccurred())

	for _, deploydeploymentToApplicationMapping := range deploymentToApplicationMappings {
		if strings.HasPrefix(deploydeploymentToApplicationMapping.Deploymenttoapplicationmapping_uid_id, "test-") {
			rowsAffected, err := dbq.DeleteDeploymentToApplicationMappingByDeplId(ctx, deploydeploymentToApplicationMapping.Deploymenttoapplicationmapping_uid_id)
			Expect(err).ToNot(HaveOccurred())

			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var applications []Application
	err = dbq.UnsafeListAllApplications(ctx, &applications)
	Expect(err).ToNot(HaveOccurred())

	for _, application := range applications {
		if strings.HasPrefix(application.Application_id, "test-") {
			rowsAffected, err := dbq.DeleteApplicationById(ctx, application.Application_id)
			Expect(err).ToNot(HaveOccurred())
			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var clusterAccess []ClusterAccess
	err = dbq.UnsafeListAllClusterAccess(ctx, &clusterAccess)
	Expect(err).ToNot(HaveOccurred())

	for _, clusterAccess := range clusterAccess {
		if strings.HasPrefix(clusterAccess.Clusteraccess_managed_environment_id, "test-") {
			rowsAffected, err := dbq.DeleteClusterAccessById(ctx, clusterAccess.Clusteraccess_user_id,
				clusterAccess.Clusteraccess_managed_environment_id,
				clusterAccess.Clusteraccess_gitops_engine_instance_id)
			Expect(err).ToNot(HaveOccurred())

			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var engineInstances []GitopsEngineInstance
	err = dbq.UnsafeListAllGitopsEngineInstances(ctx, &engineInstances)
	Expect(err).ToNot(HaveOccurred())

	for _, gitopsEngineInstance := range engineInstances {
		if strings.HasPrefix(gitopsEngineInstance.Gitopsengineinstance_id, "test-") || strings.HasPrefix(gitopsEngineInstance.Namespace_name, "test-") {

			rowsAffected, err := dbq.DeleteGitopsEngineInstanceById(ctx, gitopsEngineInstance.Gitopsengineinstance_id)

			Expect(err).ToNot(HaveOccurred())

			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var engineClusters []GitopsEngineCluster
	err = dbq.UnsafeListAllGitopsEngineClusters(ctx, &engineClusters)
	Expect(err).ToNot(HaveOccurred())

	for _, engineCluster := range engineClusters {
		if strings.HasPrefix(engineCluster.Gitopsenginecluster_id, "test-") {
			rowsAffected, err := dbq.DeleteGitopsEngineClusterById(ctx, engineCluster.Gitopsenginecluster_id)
			Expect(err).ToNot(HaveOccurred())

			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var managedEnvironments []ManagedEnvironment
	err = dbq.UnsafeListAllManagedEnvironments(ctx, &managedEnvironments)
	Expect(err).ToNot(HaveOccurred())

	for _, managedEnvironment := range managedEnvironments {
		if strings.HasPrefix(managedEnvironment.Managedenvironment_id, "test-") {
			rowsAffected, err := dbq.DeleteManagedEnvironmentById(ctx, managedEnvironment.Managedenvironment_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(rowsAffected).Should(Equal(1))
		}
	}

	var clusterCredentials []ClusterCredentials
	err = dbq.UnsafeListAllClusterCredentials(ctx, &clusterCredentials)
	Expect(err).ToNot(HaveOccurred())

	for _, clusterCredential := range clusterCredentials {
		if strings.HasPrefix(clusterCredential.Clustercredentials_cred_id, "test-") {
			rowsAffected, err := dbq.DeleteClusterCredentialsById(ctx, clusterCredential.Clustercredentials_cred_id)
			Expect(err).ToNot(HaveOccurred())

			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var clusterUsers []ClusterUser
	err = dbq.UnsafeListAllClusterUsers(ctx, &clusterUsers)

	if Expect(err).ToNot(HaveOccurred()) {
		for _, user := range clusterUsers {
			if strings.HasPrefix(user.Clusteruser_id, "test-") {
				rowsAffected, err := dbq.DeleteClusterUserById(ctx, user.Clusteruser_id)
				Expect(rowsAffected).Should(Equal(1), "expected deletion of "+user.Clusteruser_id+" to succeed.")
				Expect(err).ToNot(HaveOccurred())
			}
		}
	}

	err = dbq.CreateClusterUser(ctx, testClusterUser)
	Expect(err).ToNot(HaveOccurred())

	var kubernetesToDBResourceMappings []KubernetesToDBResourceMapping
	err = dbq.UnsafeListAllKubernetesResourceToDBResourceMapping(ctx, &kubernetesToDBResourceMappings)
	Expect(err).ToNot(HaveOccurred())

	for i := range kubernetesToDBResourceMappings {
		item := kubernetesToDBResourceMappings[i]

		if strings.HasPrefix(item.KubernetesResourceUID, "test-") || strings.HasPrefix(item.DBRelationKey, "test-") {
			rowsAffected, err := dbq.DeleteKubernetesResourceToDBResourceMapping(ctx, &item)

			Expect(err).ToNot(HaveOccurred())
			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	var apiCRToDatabaseMappings []APICRToDatabaseMapping
	err = dbq.UnsafeListAllAPICRToDatabaseMappings(ctx, &apiCRToDatabaseMappings)
	Expect(err).ToNot(HaveOccurred())
	for idx := range apiCRToDatabaseMappings {
		item := apiCRToDatabaseMappings[idx]
		if strings.HasPrefix(item.APIResourceUID, "test-") || strings.HasPrefix(item.DBRelationKey, "test-") {
			rowsAffected, err := dbq.DeleteAPICRToDatabaseMapping(ctx, &item)
			Expect(err).ToNot(HaveOccurred())
			if err == nil {
				Expect(rowsAffected).Should(Equal(1))
			}
		}
	}

	return nil
}

func removeAnyRepositoryCredentialsTestEntries(ctx context.Context, dbq AllDatabaseQueries) error {
	var repositoryCredentials []RepositoryCredentials
	var rowsAffected int

	err := dbq.UnsafeListAllRepositoryCredentials(ctx, &repositoryCredentials)
	Expect(err).ToNot(HaveOccurred())

	for _, repoCred := range repositoryCredentials {
		if strings.HasPrefix(repoCred.RepositoryCredentialsID, "test-") {
			rowsAffected, err = dbq.DeleteRepositoryCredentialsByID(ctx, repoCred.RepositoryCredentialsID)
			Expect(rowsAffected).Should(Equal(1))
			Expect(err).ToNot(HaveOccurred())
		}
	}

	return err
}
