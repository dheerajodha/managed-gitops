package eventloop

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-appstudio/managed-gitops/backend-shared/util/fauxargocd"
	"github.com/redhat-appstudio/managed-gitops/backend-shared/util/tests"
	"github.com/redhat-appstudio/managed-gitops/backend/eventloop/shared_resource_loop"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	managedgitopsv1alpha1 "github.com/redhat-appstudio/managed-gitops/backend-shared/apis/managed-gitops/v1alpha1"
	db "github.com/redhat-appstudio/managed-gitops/backend-shared/db"
	sharedoperations "github.com/redhat-appstudio/managed-gitops/backend-shared/util/operations"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logger "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("DB Clean-up Function Tests", func() {
	Context("Testing cleanOrphanedEntriesfromTable_DTAM function.", func() {

		var log logr.Logger
		var ctx context.Context
		var dbq db.AllDatabaseQueries
		var k8sClient client.WithWatch
		var application db.Application
		var syncOperation db.SyncOperation
		var applicationState db.ApplicationState
		var managedEnvironment *db.ManagedEnvironment
		var gitopsEngineInstance *db.GitopsEngineInstance
		var gitopsDepl managedgitopsv1alpha1.GitOpsDeployment
		var deploymentToApplicationMapping db.DeploymentToApplicationMapping

		BeforeEach(func() {
			scheme,
				argocdNamespace,
				kubesystemNamespace,
				apiNamespace,
				err := tests.GenericTestSetup()
			Expect(err).ToNot(HaveOccurred())

			// Create fake client
			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
				Build()

			err = db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx = context.Background()
			log = logger.FromContext(ctx)
			dbq, err = db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())

			_, managedEnvironment, _, gitopsEngineInstance, _, err = db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			// Create Application entry
			application = db.Application{
				Application_id:          "test-my-application",
				Name:                    "my-application",
				Spec_field:              "{}",
				Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
				Managed_environment_id:  managedEnvironment.Managedenvironment_id,
			}
			err = dbq.CreateApplication(ctx, &application)
			Expect(err).ToNot(HaveOccurred())

			// Create ApplicationState entry
			applicationState = db.ApplicationState{
				Applicationstate_application_id: application.Application_id,
				Health:                          "Healthy",
				Sync_Status:                     "Synced",
				ReconciledState:                 "Healthy",
			}
			err = dbq.CreateApplicationState(ctx, &applicationState)
			Expect(err).ToNot(HaveOccurred())

			gitopsDepl = managedgitopsv1alpha1.GitOpsDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-namespace",
					UID:       "test-" + uuid.NewUUID(),
				},
				Spec: managedgitopsv1alpha1.GitOpsDeploymentSpec{
					Source: managedgitopsv1alpha1.ApplicationSource{},
					Type:   managedgitopsv1alpha1.GitOpsDeploymentSpecType_Automated,
				},
			}

			// Create DeploymentToApplicationMapping entry
			deploymentToApplicationMapping = db.DeploymentToApplicationMapping{
				Deploymenttoapplicationmapping_uid_id: string(gitopsDepl.UID),
				Application_id:                        application.Application_id,
				DeploymentName:                        gitopsDepl.Name,
				DeploymentNamespace:                   gitopsDepl.Namespace,
				NamespaceUID:                          "demo-namespace",
			}
			err = dbq.CreateDeploymentToApplicationMapping(ctx, &deploymentToApplicationMapping)
			Expect(err).ToNot(HaveOccurred())

			// Create GitOpsDeployment CR in cluster
			err = k8sClient.Create(context.Background(), &gitopsDepl)
			Expect(err).ToNot(HaveOccurred())

			// Create SyncOperation entry
			syncOperation = db.SyncOperation{
				SyncOperation_id:    "test-syncOperation",
				Application_id:      application.Application_id,
				Revision:            "master",
				DeploymentNameField: deploymentToApplicationMapping.DeploymentName,
				DesiredState:        "Synced",
			}
			err = dbq.CreateSyncOperation(ctx, &syncOperation)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete any of the database entries as long as the GitOpsDeployment CR is present in cluster, and the UID matches the DTAM value", func() {
			defer dbq.CloseDatabase()

			By("Call cleanOrphanedEntriesfromTable_DTAM function to check delete DB entries if GitOpsDeployment CR is not present.")
			cleanOrphanedEntriesfromTable_DTAM(ctx, dbq, k8sClient, true, log)

			By("Verify that no entry is deleted from DB.")
			err := dbq.GetApplicationStateById(ctx, &applicationState)
			Expect(err).ToNot(HaveOccurred())

			err = dbq.GetSyncOperationById(ctx, &syncOperation)
			Expect(err).ToNot(HaveOccurred())
			Expect(syncOperation.Application_id).NotTo(BeEmpty())

			err = dbq.GetDeploymentToApplicationMappingByApplicationId(ctx, &deploymentToApplicationMapping)
			Expect(err).ToNot(HaveOccurred())

			err = dbq.GetApplicationById(ctx, &application)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should delete related database entries from DB, if the GitOpsDeployment CRs of the DTAM is not present on cluster.", func() {
			defer dbq.CloseDatabase()

			// Create another Application entry
			applicationOne := application
			applicationOne.Application_id = "test-my-application-1"
			applicationOne.Name = "my-application-1"
			err := dbq.CreateApplication(ctx, &applicationOne)
			Expect(err).ToNot(HaveOccurred())

			// Create another DeploymentToApplicationMapping entry
			deploymentToApplicationMappingOne := deploymentToApplicationMapping
			deploymentToApplicationMappingOne.Deploymenttoapplicationmapping_uid_id = "test-" + string(uuid.NewUUID())
			deploymentToApplicationMappingOne.Application_id = applicationOne.Application_id
			deploymentToApplicationMappingOne.DeploymentName = "test-deployment-1"
			err = dbq.CreateDeploymentToApplicationMapping(ctx, &deploymentToApplicationMappingOne)
			Expect(err).ToNot(HaveOccurred())

			// Create another ApplicationState entry
			applicationStateOne := applicationState
			applicationStateOne.Applicationstate_application_id = applicationOne.Application_id
			err = dbq.CreateApplicationState(ctx, &applicationStateOne)
			Expect(err).ToNot(HaveOccurred())

			// Create another SyncOperation entry
			syncOperationOne := syncOperation
			syncOperationOne.SyncOperation_id = "test-syncOperation-1"
			syncOperationOne.Application_id = applicationOne.Application_id
			syncOperationOne.DeploymentNameField = deploymentToApplicationMappingOne.DeploymentName
			err = dbq.CreateSyncOperation(ctx, &syncOperationOne)
			Expect(err).ToNot(HaveOccurred())

			By("Call cleanOrphanedEntriesfromTable_DTAM function to check/delete DB entries if GitOpsDeployment CR is not present.")
			cleanOrphanedEntriesfromTable_DTAM(ctx, dbq, k8sClient, true, log)

			By("Verify that entries for the GitOpsDeployment which is available in cluster, are not deleted from DB.")

			err = dbq.GetApplicationStateById(ctx, &applicationState)
			Expect(err).ToNot(HaveOccurred())

			err = dbq.GetSyncOperationById(ctx, &syncOperation)
			Expect(err).ToNot(HaveOccurred())
			Expect(syncOperation.Application_id).To(Equal(application.Application_id))

			err = dbq.GetDeploymentToApplicationMappingByApplicationId(ctx, &deploymentToApplicationMapping)
			Expect(err).ToNot(HaveOccurred())

			err = dbq.GetApplicationById(ctx, &application)
			Expect(err).ToNot(HaveOccurred())

			By("Verify that entries for the GitOpsDeployment which is not available in cluster, are deleted from DB.")

			err = dbq.GetApplicationStateById(ctx, &applicationStateOne)
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			err = dbq.GetSyncOperationById(ctx, &syncOperationOne)
			Expect(err).ToNot(HaveOccurred())
			Expect(syncOperationOne.Application_id).To(BeEmpty())

			err = dbq.GetDeploymentToApplicationMappingByApplicationId(ctx, &deploymentToApplicationMappingOne)
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			err = dbq.GetApplicationById(ctx, &applicationOne)
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())
		})

		It("Should delete the DTAM if the GitOpsDeployment CR if it is present, but the UID doesn't match what is in the DTAM", func() {
			defer dbq.CloseDatabase()

			By("We ensure that the GitOpsDeployment still has the same name, but has a different UID. This simulates a new GitOpsDeployment with the same name/namespace.")
			newUID := "test-" + uuid.NewUUID()
			gitopsDepl.UID = newUID
			err := k8sClient.Update(ctx, &gitopsDepl)
			Expect(err).ToNot(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(&gitopsDepl), &gitopsDepl)
			Expect(err).ToNot(HaveOccurred())
			Expect(gitopsDepl.UID).To(Equal(newUID))

			By("calling cleanOrphanedEntriesfromTable_DTAM function to check delete DB entries if GitOpsDeployment CR is not present.")
			cleanOrphanedEntriesfromTable_DTAM(ctx, dbq, k8sClient, true, log)

			By("Verify that entries for the GitOpsDeployment which is not available in cluster, are deleted from DB.")

			err = dbq.GetApplicationStateById(ctx, &applicationState)
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			err = dbq.GetSyncOperationById(ctx, &syncOperation)
			Expect(err).ToNot(HaveOccurred())
			Expect(syncOperation.Application_id).To(BeEmpty())

			err = dbq.GetDeploymentToApplicationMappingByApplicationId(ctx, &deploymentToApplicationMapping)
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			err = dbq.GetApplicationById(ctx, &application)
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

		})
	})

	Context("Testing cleanOrphanedEntriesfromTable_ACTDM function.", func() {
		Context("Testing cleanOrphanedEntriesfromTable_ACTDM function for ManagedEnvironment CR.", func() {
			var log logr.Logger
			var ctx context.Context
			var dbq db.AllDatabaseQueries
			var k8sClient client.WithWatch
			var clusterCredentialsDb db.ClusterCredentials
			var managedEnvironmentDb db.ManagedEnvironment
			var apiCRToDatabaseMappingDb db.APICRToDatabaseMapping
			var managedEnvCr *managedgitopsv1alpha1.GitOpsDeploymentManagedEnvironment

			BeforeEach(func() {
				scheme,
					argocdNamespace,
					kubesystemNamespace,
					apiNamespace,
					err := tests.GenericTestSetup()
				Expect(err).ToNot(HaveOccurred())

				// Create fake client
				k8sClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
					Build()

				err = db.SetupForTestingDBGinkgo()
				Expect(err).ToNot(HaveOccurred())

				ctx = context.Background()
				log = logger.FromContext(ctx)
				dbq, err = db.NewUnsafePostgresDBQueries(true, true)
				Expect(err).ToNot(HaveOccurred())

				By("Create required CRs in Cluster.")

				// Create Secret in Cluster
				secretCr := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-managed-env-secret",
						Namespace: "test-k8s-namespace",
					},
					Type:       "managed-gitops.redhat.com/managed-environment",
					StringData: map[string]string{shared_resource_loop.KubeconfigKey: "abc"},
				}
				err = k8sClient.Create(context.Background(), secretCr)
				Expect(err).ToNot(HaveOccurred())

				// Create GitOpsDeploymentManagedEnvironment CR in cluster
				managedEnvCr = &managedgitopsv1alpha1.GitOpsDeploymentManagedEnvironment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-env-" + string(uuid.NewUUID()),
						Namespace: "test-k8s-namespace",
						UID:       uuid.NewUUID(),
					},
					Spec: managedgitopsv1alpha1.GitOpsDeploymentManagedEnvironmentSpec{
						APIURL:                     "",
						ClusterCredentialsSecret:   secretCr.Name,
						AllowInsecureSkipTLSVerify: true,
					},
				}
				err = k8sClient.Create(context.Background(), managedEnvCr)
				Expect(err).ToNot(HaveOccurred())

				By("Create required DB entries.")

				// Create DB entry for ClusterCredentials
				clusterCredentialsDb = db.ClusterCredentials{
					Clustercredentials_cred_id:  "test-" + string(uuid.NewUUID()),
					Host:                        "host",
					Kube_config:                 "kube-config",
					Kube_config_context:         "kube-config-context",
					Serviceaccount_bearer_token: "serviceaccount_bearer_token",
					Serviceaccount_ns:           "Serviceaccount_ns",
				}
				err = dbq.CreateClusterCredentials(ctx, &clusterCredentialsDb)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for ManagedEnvironment
				managedEnvironmentDb = db.ManagedEnvironment{
					Managedenvironment_id: "test-env-" + string(managedEnvCr.UID),
					Clustercredentials_id: clusterCredentialsDb.Clustercredentials_cred_id,
					Name:                  managedEnvCr.Name,
				}
				err = dbq.CreateManagedEnvironment(ctx, &managedEnvironmentDb)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for APICRToDatabaseMapping
				apiCRToDatabaseMappingDb = db.APICRToDatabaseMapping{
					APIResourceType:      db.APICRToDatabaseMapping_ResourceType_GitOpsDeploymentManagedEnvironment,
					APIResourceUID:       string(managedEnvCr.UID),
					APIResourceName:      managedEnvCr.Name,
					APIResourceNamespace: managedEnvCr.Namespace,
					NamespaceUID:         "test-" + string(uuid.NewUUID()),
					DBRelationType:       db.APICRToDatabaseMapping_DBRelationType_ManagedEnvironment,
					DBRelationKey:        managedEnvironmentDb.Managedenvironment_id,
				}
			})

			It("Should not delete any of the database entries as long as the Managed Environment CR is present in cluster, and the UID matches the APICRToDatabaseMapping value", func() {
				defer dbq.CloseDatabase()

				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, nil, true, log)

				By("Verify that no entry is deleted from DB.")
				err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDb)
				Expect(err).ToNot(HaveOccurred())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should delete related database entries from DB, if the Managed Environment CR of the APICRToDatabaseMapping is not present on cluster.", func() {
				defer dbq.CloseDatabase()

				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for ClusterCredentials
				clusterCredentialsDb.Clustercredentials_cred_id = "test-" + string(uuid.NewUUID())
				err = dbq.CreateClusterCredentials(ctx, &clusterCredentialsDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another ManagedEnvironment entry
				managedEnvironmentDbTemp := managedEnvironmentDb
				managedEnvironmentDb.Name = "test-env-" + string(uuid.NewUUID())
				managedEnvironmentDb.Managedenvironment_id = "test-" + string(uuid.NewUUID())
				managedEnvironmentDb.Clustercredentials_id = clusterCredentialsDb.Clustercredentials_cred_id
				err = dbq.CreateManagedEnvironment(ctx, &managedEnvironmentDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another APICRToDatabaseMapping entry
				apiCRToDatabaseMappingDbTemp := apiCRToDatabaseMappingDb
				apiCRToDatabaseMappingDb.DBRelationKey = managedEnvironmentDb.Managedenvironment_id
				apiCRToDatabaseMappingDb.APIResourceUID = "test-" + string(uuid.NewUUID())
				apiCRToDatabaseMappingDb.APIResourceName = managedEnvironmentDb.Name
				err = dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, nil, true, log)

				By("Verify that entries for the ManagedEnvironment which is not available in cluster, are deleted from DB.")

				err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				By("Verify that entries for the ManagedEnvironment which is available in cluster, are not deleted from DB.")

				err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDbTemp)
				Expect(err).ToNot(HaveOccurred())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDbTemp)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should delete related database entries from DB, if the Managed Environment CR is present in cluster, but the UID doesn't match what is in the APICRToDatabaseMapping", func() {
				defer dbq.CloseDatabase()

				// Create another ACTDB entry
				apiCRToDatabaseMappingDb.APIResourceUID = "test-" + string(uuid.NewUUID())
				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, nil, true, log)

				By("Verify that entries for the ManagedEnvironment which is not available in cluster, are deleted from DB.")

				err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())
			})

			It("Should delete related database entries from DB, if the Managed Environment CR of the APICRToDatabaseMapping is not present on cluster and it should create Operation to inform.", func() {
				defer dbq.CloseDatabase()

				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for ClusterCredentials
				clusterCredentialsDb.Clustercredentials_cred_id = "test-" + string(uuid.NewUUID())
				err = dbq.CreateClusterCredentials(ctx, &clusterCredentialsDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another ManagedEnvironment entry
				managedEnvironmentDbTemp := managedEnvironmentDb
				managedEnvironmentDb.Name = "test-env-" + string(uuid.NewUUID())
				managedEnvironmentDb.Managedenvironment_id = "test-" + string(uuid.NewUUID())
				managedEnvironmentDb.Clustercredentials_id = clusterCredentialsDb.Clustercredentials_cred_id
				err = dbq.CreateManagedEnvironment(ctx, &managedEnvironmentDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another APICRToDatabaseMapping entry
				apiCRToDatabaseMappingDbTemp := apiCRToDatabaseMappingDb
				apiCRToDatabaseMappingDb.DBRelationKey = managedEnvironmentDb.Managedenvironment_id
				apiCRToDatabaseMappingDb.APIResourceUID = "test-" + string(uuid.NewUUID())
				apiCRToDatabaseMappingDb.APIResourceName = managedEnvironmentDb.Name
				err = dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				_, _, engineCluster, _, _, err := db.CreateSampleData(dbq)
				Expect(err).ToNot(HaveOccurred())
				gitopsEngineInstance := &db.GitopsEngineInstance{
					Gitopsengineinstance_id: "test-fake-instance-id",
					Namespace_name:          "gitops-service-argocd",
					Namespace_uid:           "test-fake-instance-namespace-914",
					EngineCluster_id:        engineCluster.Gitopsenginecluster_id,
				}
				err = dbq.CreateGitopsEngineInstance(ctx, gitopsEngineInstance)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for Application
				applicationDb := &db.Application{
					Application_id:          "test-app-" + string(uuid.NewUUID()),
					Name:                    "test-app",
					Spec_field:              "{}",
					Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
					Managed_environment_id:  managedEnvironmentDb.Managedenvironment_id,
				}
				err = dbq.CreateApplication(ctx, applicationDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, MockSRLK8sClientFactory{fakeClient: k8sClient}, true, log)

				By("Verify that entries for the ManagedEnvironment which is not available in cluster, are deleted from DB.")

				err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				By("Verify that entries for the ManagedEnvironment which is available in cluster, are not deleted from DB.")

				err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDbTemp)
				Expect(err).ToNot(HaveOccurred())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDbTemp)
				Expect(err).ToNot(HaveOccurred())

				By("Verify that Operation for the ManagedEnvironment is created.")

				var operationlist []db.Operation
				err = dbq.ListOperationsByResourceIdAndTypeAndOwnerId(ctx, managedEnvironmentDb.Managedenvironment_id, db.OperationResourceType_ManagedEnvironment, &operationlist, "cluster-agent-application-sync-user")
				Expect(err).ToNot(HaveOccurred())
				Expect(operationlist).ShouldNot(BeEmpty())
				Expect(operationlist[0].Resource_id).To(Equal(managedEnvironmentDb.Managedenvironment_id))
			})
		})

		Context("Testing cleanOrphanedEntriesfromTable_ACTDM function for RepositoryCredential CR.", func() {
			var log logr.Logger
			var ctx context.Context
			var dbq db.AllDatabaseQueries
			var k8sClient client.WithWatch
			var clusterUserDb *db.ClusterUser
			var apiCRToDatabaseMappingDb db.APICRToDatabaseMapping
			var gitopsRepositoryCredentialsDb db.RepositoryCredentials
			var repoCredentialCr managedgitopsv1alpha1.GitOpsDeploymentRepositoryCredential

			BeforeEach(func() {
				scheme,
					argocdNamespace,
					kubesystemNamespace,
					apiNamespace,
					err := tests.GenericTestSetup()
				Expect(err).ToNot(HaveOccurred())

				// Create fake client
				k8sClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
					Build()

				err = db.SetupForTestingDBGinkgo()
				Expect(err).ToNot(HaveOccurred())

				ctx = context.Background()
				log = logger.FromContext(ctx)
				dbq, err = db.NewUnsafePostgresDBQueries(true, true)
				Expect(err).ToNot(HaveOccurred())

				By("Create required CRs in Cluster.")

				// Create Secret in Cluster
				secretCr := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "test-k8s-namespace",
					},
					Type: "managed-gitops.redhat.com/managed-environment",
					StringData: map[string]string{
						"username": "test-user",
						"password": "test@123",
					},
				}
				err = k8sClient.Create(context.Background(), secretCr)
				Expect(err).ToNot(HaveOccurred())

				// Create GitOpsDeploymentRepositoryCredential in Cluster
				repoCredentialCr = managedgitopsv1alpha1.GitOpsDeploymentRepositoryCredential{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-repo-" + string(uuid.NewUUID()),
						Namespace: "test-k8s-namespace",
						UID:       uuid.NewUUID(),
					},
					Spec: managedgitopsv1alpha1.GitOpsDeploymentRepositoryCredentialSpec{
						Repository: "https://test-private-url",
						Secret:     "test-secret",
					},
				}
				err = k8sClient.Create(context.Background(), &repoCredentialCr)
				Expect(err).ToNot(HaveOccurred())

				By("Create required DB entries.")

				_, _, engineCluster, _, _, err := db.CreateSampleData(dbq)
				Expect(err).ToNot(HaveOccurred())
				gitopsEngineInstance := &db.GitopsEngineInstance{
					Gitopsengineinstance_id: "test-fake-instance-id",
					Namespace_name:          "test-k8s-namespace",
					Namespace_uid:           "test-fake-instance-namespace-914",
					EngineCluster_id:        engineCluster.Gitopsenginecluster_id,
				}
				err = dbq.CreateGitopsEngineInstance(ctx, gitopsEngineInstance)
				Expect(err).ToNot(HaveOccurred())
				// Create DB entry for ClusterUser
				clusterUserDb = &db.ClusterUser{
					Clusteruser_id: "test-repocred-user-id",
					User_name:      "test-repocred-user",
				}
				err = dbq.CreateClusterUser(ctx, clusterUserDb)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for RepositoryCredentials
				gitopsRepositoryCredentialsDb = db.RepositoryCredentials{
					RepositoryCredentialsID: "test-repo-" + string(uuid.NewUUID()),
					UserID:                  clusterUserDb.Clusteruser_id,
					PrivateURL:              "https://test-private-url",
					AuthUsername:            "test-auth-username",
					AuthPassword:            "test-auth-password",
					AuthSSHKey:              "test-auth-ssh-key",
					SecretObj:               "test-secret-obj",
					EngineClusterID:         gitopsEngineInstance.Gitopsengineinstance_id,
				}
				err = dbq.CreateRepositoryCredentials(ctx, &gitopsRepositoryCredentialsDb)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for APICRToDatabaseMapping
				apiCRToDatabaseMappingDb = db.APICRToDatabaseMapping{
					APIResourceType:      db.APICRToDatabaseMapping_ResourceType_GitOpsDeploymentRepositoryCredential,
					APIResourceUID:       string(repoCredentialCr.UID),
					APIResourceName:      repoCredentialCr.Name,
					APIResourceNamespace: repoCredentialCr.Namespace,
					NamespaceUID:         "test-" + string(uuid.NewUUID()),
					DBRelationType:       db.APICRToDatabaseMapping_DBRelationType_ManagedEnvironment,
					DBRelationKey:        gitopsRepositoryCredentialsDb.RepositoryCredentialsID,
				}
			})

			It("Should not delete any of the database entries as long as the RepositoryCredentials CR is present in cluster, and the UID matches the APICRToDatabaseMapping value", func() {
				defer dbq.CloseDatabase()

				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, nil, true, log)

				By("Verify that no entry is deleted from DB.")
				_, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDb.RepositoryCredentialsID)
				Expect(err).ToNot(HaveOccurred())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should delete related database entries from DB, if the RepositoryCredentials CR of the APICRToDatabaseMapping is not present on cluster.", func() {
				defer dbq.CloseDatabase()

				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another GitopsRepositoryCredentials entry in Db
				gitopsRepositoryCredentialsDbTemp := gitopsRepositoryCredentialsDb
				gitopsRepositoryCredentialsDb.RepositoryCredentialsID = "test-repo-" + string(uuid.NewUUID())
				err = dbq.CreateRepositoryCredentials(ctx, &gitopsRepositoryCredentialsDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another APICRToDatabaseMapping entry in Db
				apiCRToDatabaseMappingTemp := apiCRToDatabaseMappingDb
				apiCRToDatabaseMappingDb.DBRelationKey = gitopsRepositoryCredentialsDb.RepositoryCredentialsID
				apiCRToDatabaseMappingDb.APIResourceUID = "test-" + string(uuid.NewUUID())
				apiCRToDatabaseMappingDb.APIResourceName = "test-" + string(uuid.NewUUID())
				err = dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, nil, true, log)

				By("Verify that entries for the GitOpsDeployment which is not available in cluster, are deleted from DB.")

				_, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDb.RepositoryCredentialsID)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				By("Verify that entries for the RepositoryCredentials which is available in cluster, are not deleted from DB.")

				_, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDbTemp.RepositoryCredentialsID)
				Expect(err).ToNot(HaveOccurred())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingTemp)
				Expect(err).ToNot(HaveOccurred())

				By("Verify that Operation for the RepositoryCredentials is created in cluster and DB.")

				var specialClusterUser db.ClusterUser
				err = dbq.GetOrCreateSpecialClusterUser(context.Background(), &specialClusterUser)
				Expect(err).ToNot(HaveOccurred())

				var operationlist []db.Operation
				err = dbq.ListOperationsByResourceIdAndTypeAndOwnerId(ctx, gitopsRepositoryCredentialsDb.RepositoryCredentialsID, db.OperationResourceType_RepositoryCredentials, &operationlist, specialClusterUser.Clusteruser_id)
				Expect(err).ToNot(HaveOccurred())
				Expect(operationlist).ShouldNot(BeEmpty())

				objectMeta := metav1.ObjectMeta{
					Name:      sharedoperations.GenerateOperationCRName(operationlist[0]),
					Namespace: repoCredentialCr.Namespace,
				}
				k8sOperation := managedgitopsv1alpha1.Operation{ObjectMeta: objectMeta}

				err = k8sClient.Get(context.Background(), types.NamespacedName{Namespace: objectMeta.Namespace, Name: objectMeta.Name}, &k8sOperation)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should delete related database entries from DB, if the RepositoryCredentials CR is present in cluster, but the UID doesn't match what is in the APICRToDatabaseMapping", func() {
				defer dbq.CloseDatabase()

				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another RepositoryCredentials entry in Db
				gitopsRepositoryCredentialsDb.RepositoryCredentialsID = "test-repo-" + string(uuid.NewUUID())
				err = dbq.CreateRepositoryCredentials(ctx, &gitopsRepositoryCredentialsDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another APICRToDatabaseMapping entry in Db
				apiCRToDatabaseMappingDb.DBRelationKey = gitopsRepositoryCredentialsDb.RepositoryCredentialsID
				apiCRToDatabaseMappingDb.APIResourceUID = "test-" + string(uuid.NewUUID())
				err = dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, nil, true, log)

				By("Verify that entries for the RepositoryCredentials which is not available in cluster, are deleted from DB.")

				_, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDb.RepositoryCredentialsID)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())
			})
		})

		Context("Testing cleanOrphanedEntriesfromTable_ACTDM function for GitOpsDeploymentSyncRun CR.", func() {
			var log logr.Logger
			var ctx context.Context
			var dbq db.AllDatabaseQueries
			var k8sClient client.WithWatch
			var syncOperationDb db.SyncOperation
			var apiCRToDatabaseMappingDb db.APICRToDatabaseMapping
			var gitopsDeplSyncRunCr managedgitopsv1alpha1.GitOpsDeploymentSyncRun

			BeforeEach(func() {
				scheme,
					argocdNamespace,
					kubesystemNamespace,
					apiNamespace,
					err := tests.GenericTestSetup()
				Expect(err).ToNot(HaveOccurred())

				// Create fake client
				k8sClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
					Build()

				err = db.SetupForTestingDBGinkgo()
				Expect(err).ToNot(HaveOccurred())

				ctx = context.Background()
				log = logger.FromContext(ctx)
				dbq, err = db.NewUnsafePostgresDBQueries(true, true)
				Expect(err).ToNot(HaveOccurred())

				By("Create required CRs in Cluster.")

				// Create GitOpsDeploymentSyncRun in Cluster
				gitopsDeplSyncRunCr = managedgitopsv1alpha1.GitOpsDeploymentSyncRun{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gitopsdeployment-syncrun",
						Namespace: "test-k8s-namespace",
						UID:       uuid.NewUUID(),
					},
					Spec: managedgitopsv1alpha1.GitOpsDeploymentSyncRunSpec{
						GitopsDeploymentName: "test-app",
						RevisionID:           "HEAD",
					},
				}

				err = k8sClient.Create(context.Background(), &gitopsDeplSyncRunCr)
				Expect(err).ToNot(HaveOccurred())

				By("Create required DB entries.")

				_, managedEnvironment, engineCluster, _, _, err := db.CreateSampleData(dbq)
				Expect(err).ToNot(HaveOccurred())

				gitopsEngineInstance := &db.GitopsEngineInstance{
					Gitopsengineinstance_id: "test-fake-instance-id",
					Namespace_name:          "test-k8s-namespace",
					Namespace_uid:           "test-fake-instance-namespace-914",
					EngineCluster_id:        engineCluster.Gitopsenginecluster_id,
				}
				err = dbq.CreateGitopsEngineInstance(ctx, gitopsEngineInstance)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for Application
				applicationDb := &db.Application{
					Application_id:          "test-app-" + string(uuid.NewUUID()),
					Name:                    gitopsDeplSyncRunCr.Spec.GitopsDeploymentName,
					Spec_field:              "{}",
					Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
					Managed_environment_id:  managedEnvironment.Managedenvironment_id,
				}
				err = dbq.CreateApplication(ctx, applicationDb)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for SyncOperation
				syncOperationDb = db.SyncOperation{
					SyncOperation_id:    "test-op-" + string(uuid.NewUUID()),
					Application_id:      applicationDb.Application_id,
					DeploymentNameField: "test-depl-" + string(uuid.NewUUID()),
					Revision:            "Head",
					DesiredState:        "Terminated",
				}
				err = dbq.CreateSyncOperation(ctx, &syncOperationDb)
				Expect(err).ToNot(HaveOccurred())

				// Create DB entry for APICRToDatabaseMappingapiCRToDatabaseMapping
				apiCRToDatabaseMappingDb = db.APICRToDatabaseMapping{
					APIResourceType:      db.APICRToDatabaseMapping_ResourceType_GitOpsDeploymentSyncRun,
					APIResourceUID:       string(gitopsDeplSyncRunCr.UID),
					APIResourceName:      gitopsDeplSyncRunCr.Name,
					APIResourceNamespace: gitopsDeplSyncRunCr.Namespace,
					NamespaceUID:         "test-" + string(uuid.NewUUID()),
					DBRelationType:       db.APICRToDatabaseMapping_DBRelationType_ManagedEnvironment,
					DBRelationKey:        syncOperationDb.SyncOperation_id,
				}
			})

			It("Should not delete any of the database entries as long as the GitOpsDeploymentSyncRun CR is present in cluster, and the UID matches the APICRToDatabaseMapping value", func() {
				defer dbq.CloseDatabase()

				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, nil, true, log)

				By("Verify that no entry is deleted from DB.")
				err = dbq.GetSyncOperationById(ctx, &syncOperationDb)
				Expect(err).ToNot(HaveOccurred())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should delete related database entries from DB, if the GitOpsDeploymentSyncRun CR of the APICRToDatabaseMapping is not present on cluster.", func() {
				defer dbq.CloseDatabase()

				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another entry for SyncOperation
				syncOperationDbTemp := syncOperationDb
				syncOperationDb.SyncOperation_id = "test-sync-" + string(uuid.NewUUID())
				err = dbq.CreateSyncOperation(ctx, &syncOperationDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another entry for APICRToDatabaseMapping
				apiCRToDatabaseMappingDbTemp := apiCRToDatabaseMappingDb
				apiCRToDatabaseMappingDb.DBRelationKey = syncOperationDb.SyncOperation_id
				apiCRToDatabaseMappingDb.APIResourceUID = "test-" + string(uuid.NewUUID())
				apiCRToDatabaseMappingDb.APIResourceName = "test-" + string(uuid.NewUUID())
				err = dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, MockSRLK8sClientFactory{fakeClient: k8sClient}, true, log)

				By("Verify that entries for the GitOpsDeploymentSyncRun which is not available in cluster, are deleted from DB.")

				err = dbq.GetSyncOperationById(ctx, &syncOperationDb)
				Expect(err).To(HaveOccurred())
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).To(HaveOccurred())
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				By("Verify that entries for the GitOpsDeploymentSyncRun which is available in cluster, are not deleted from DB.")

				err = dbq.GetSyncOperationById(ctx, &syncOperationDbTemp)
				Expect(err).ToNot(HaveOccurred())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDbTemp)
				Expect(err).ToNot(HaveOccurred())

				By("Verify that Operation for the GitOpsDeploymentSyncRun is created in cluster and DB.")

				var specialClusterUser db.ClusterUser
				err = dbq.GetOrCreateSpecialClusterUser(context.Background(), &specialClusterUser)
				Expect(err).ToNot(HaveOccurred())

				var operationlist []db.Operation
				err = dbq.ListOperationsByResourceIdAndTypeAndOwnerId(ctx, syncOperationDb.SyncOperation_id, db.OperationResourceType_SyncOperation, &operationlist, specialClusterUser.Clusteruser_id)
				Expect(err).ToNot(HaveOccurred())
				Expect(operationlist).ShouldNot(BeEmpty())

				objectMeta := metav1.ObjectMeta{
					Name:      sharedoperations.GenerateOperationCRName(operationlist[0]),
					Namespace: gitopsDeplSyncRunCr.Namespace,
				}
				k8sOperation := managedgitopsv1alpha1.Operation{ObjectMeta: objectMeta}

				err = k8sClient.Get(context.Background(), types.NamespacedName{Namespace: objectMeta.Namespace, Name: objectMeta.Name}, &k8sOperation)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should delete related database entries from DB, if the GitOpsDeploymentSyncRun CR is present in cluster, but the UID doesn't match what is in the APICRToDatabaseMapping", func() {
				defer dbq.CloseDatabase()

				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another SyncOperation DB entry
				syncOperationDb.SyncOperation_id = "test-sync-" + string(uuid.NewUUID())
				err = dbq.CreateSyncOperation(ctx, &syncOperationDb)
				Expect(err).ToNot(HaveOccurred())

				// Create another APICRToDatabaseMapping DB entry
				apiCRToDatabaseMappingDb.DBRelationKey = syncOperationDb.SyncOperation_id
				apiCRToDatabaseMappingDb.APIResourceUID = "test-" + string(uuid.NewUUID())
				err = dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, nil, true, log)

				By("Verify that entries for the GitOpsDeploymentSyncRun which is not available in cluster, are deleted from DB.")

				err = dbq.GetSyncOperationById(ctx, &syncOperationDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())
			})

			It("should delete the SyncRun CR without creating an Operation if the Application is already deleted", func() {
				defer dbq.CloseDatabase()

				apiCRToDatabaseMappingDb.APIResourceName = "fake-resource"
				err := dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
				Expect(err).ToNot(HaveOccurred())

				By("remove the Application_ID foreign key")
				rows, err := dbq.UpdateSyncOperationRemoveApplicationField(ctx, syncOperationDb.Application_id)
				Expect(err).ToNot(HaveOccurred())
				Expect(rows).To(Equal(1))

				err = dbq.GetSyncOperationById(ctx, &syncOperationDb)
				Expect(err).ToNot(HaveOccurred())
				Expect(syncOperationDb.Application_id).To(BeEmpty())

				By("Call cleanOrphanedEntriesfromTable_ACTDM function.")
				cleanOrphanedEntriesfromTable_ACTDM(ctx, dbq, k8sClient, nil, true, log)

				By("Verify that entries for the GitOpsDeploymentSyncRun which is not available in cluster, are deleted from DB.")

				err = dbq.GetSyncOperationById(ctx, &syncOperationDb)
				Expect(err).To(HaveOccurred())
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				err = dbq.GetAPICRForDatabaseUID(ctx, &apiCRToDatabaseMappingDb)
				Expect(db.IsResultNotFoundError(err)).To(BeTrue())

				By("Verify that Operation for the GitOpsDeploymentSyncRun is not created in cluster and DB.")

				var specialClusterUser db.ClusterUser
				err = dbq.GetOrCreateSpecialClusterUser(context.Background(), &specialClusterUser)
				Expect(err).ToNot(HaveOccurred())

				operationList := managedgitopsv1alpha1.OperationList{}
				err = k8sClient.List(ctx, &operationList)
				Expect(err).ToNot(HaveOccurred())
				Expect(operationList.Items).To(BeEmpty())
			})
		})
	})

	Context("Testing cleanOrphanedEntriesfromTable_Application function for Application table entries.", func() {

		var log logr.Logger
		var ctx context.Context
		var dbq db.AllDatabaseQueries
		var k8sClient client.WithWatch
		var application db.Application
		var managedEnvironment *db.ManagedEnvironment
		var gitopsEngineInstance *db.GitopsEngineInstance
		var deploymentToApplicationMapping db.DeploymentToApplicationMapping

		BeforeEach(func() {
			scheme,
				argocdNamespace,
				kubesystemNamespace,
				apiNamespace,
				err := tests.GenericTestSetup()
			Expect(err).ToNot(HaveOccurred())

			// Create fake client
			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
				Build()

			err = db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx = context.Background()
			log = logger.FromContext(ctx)
			dbq, err = db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())

			_, managedEnvironment, _, gitopsEngineInstance, _, err = db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			// Create Application entry
			application = db.Application{
				Application_id:          "test-app-" + string(uuid.NewUUID()),
				Name:                    "test-app-" + string(uuid.NewUUID()),
				Spec_field:              "{}",
				Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
				Managed_environment_id:  managedEnvironment.Managedenvironment_id,
			}
			err = dbq.CreateApplication(ctx, &application)
			Expect(err).ToNot(HaveOccurred())

			// Create DeploymentToApplicationMapping entry
			deploymentToApplicationMapping = db.DeploymentToApplicationMapping{
				Deploymenttoapplicationmapping_uid_id: "test-" + string(uuid.NewUUID()),
				Application_id:                        application.Application_id,
				DeploymentName:                        application.Name,
				DeploymentNamespace:                   "test-ns-" + string(uuid.NewUUID()),
				NamespaceUID:                          string(uuid.NewUUID()),
			}
			err = dbq.CreateDeploymentToApplicationMapping(ctx, &deploymentToApplicationMapping)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete application entry if its DTAM entry is available.", func() {
			defer dbq.CloseDatabase()

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_Application(ctx, dbq, k8sClient, true, log)

			By("Verify that no entry is deleted from DB.")

			err := dbq.GetApplicationById(ctx, &application)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should delete application entry if its DTAM entry is not available.", func() {
			defer dbq.CloseDatabase()

			By("Create application row without DTAM entry.")

			// Create dummy Application Spec to be saved in DB
			dummyApplicationSpec := fauxargocd.FauxApplication{
				FauxObjectMeta: fauxargocd.FauxObjectMeta{
					Namespace: "argocd",
				},
			}
			dummyApplicationSpecBytes, err := yaml.Marshal(dummyApplicationSpec)
			Expect(err).ToNot(HaveOccurred())

			applicationNew := db.Application{
				Application_id:          "test-app-" + string(uuid.NewUUID()),
				Name:                    "test-app-" + string(uuid.NewUUID()),
				Spec_field:              string(dummyApplicationSpecBytes),
				Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
				Managed_environment_id:  managedEnvironment.Managedenvironment_id,
			}
			err = dbq.CreateApplication(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateApplication function since CreateApplication does not allow to insert custom "Created_on" field.
			err = dbq.GetApplicationById(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field more than waitTimeforRowDelete
			applicationNew.Created_on = time.Now().Add(time.Duration(-(waitTimeforRowDelete + 1)))
			err = dbq.UpdateApplication(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_Application(ctx, dbq, k8sClient, true, log)

			By("Verify that application row without DTAM entry is deleted from DB.")

			err = dbq.GetApplicationById(ctx, &applicationNew)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			By("Verify that application row having DTAM entry is not deleted from DB.")

			err = dbq.GetApplicationById(ctx, &application)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete application entry if its DTAM entry is not available, but created time is less than wait time for deletion.", func() {
			defer dbq.CloseDatabase()

			By("Create application row without DTAM entry.")

			applicationNew := db.Application{
				Application_id:          "test-" + string(uuid.NewUUID()),
				Name:                    "test-" + string(uuid.NewUUID()),
				Spec_field:              "{}",
				Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
				Managed_environment_id:  managedEnvironment.Managedenvironment_id,
			}
			err := dbq.CreateApplication(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateApplication function since CreateApplication does not allow to insert custom "Created_on" field.
			err = dbq.GetApplicationById(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field less than waitTimeforRowDelete
			applicationNew.Created_on = time.Now().Add(time.Duration(-30) * time.Minute)
			err = dbq.UpdateApplication(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_Application(ctx, dbq, k8sClient, true, log)

			By("Verify that no application rows are deleted from DB.")

			err = dbq.GetApplicationById(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			err = dbq.GetApplicationById(ctx, &application)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should verify when an orphaned Application is deleted, ApplicationOwner is deleted as well", func() {
			defer dbq.CloseDatabase()

			By("Create application row without DTAM entry.")

			// Create dummy Application Spec to be saved in DB
			dummyApplicationSpec := fauxargocd.FauxApplication{
				FauxObjectMeta: fauxargocd.FauxObjectMeta{
					Namespace: "argocd",
				},
			}
			dummyApplicationSpecBytes, err := yaml.Marshal(dummyApplicationSpec)
			Expect(err).ToNot(HaveOccurred())

			applicationNew := db.Application{
				Application_id:          "test-app-" + string(uuid.NewUUID()),
				Name:                    "test-app-" + string(uuid.NewUUID()),
				Spec_field:              string(dummyApplicationSpecBytes),
				Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
				Managed_environment_id:  managedEnvironment.Managedenvironment_id,
			}
			err = dbq.CreateApplication(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			// Create DB entry for ClusterUser
			clusterUserDb := &db.ClusterUser{
				Clusteruser_id: "test-repocred-user-id",
				User_name:      "test-repocred-user",
			}
			err = dbq.CreateClusterUser(ctx, clusterUserDb)
			Expect(err).ToNot(HaveOccurred())

			applicationOwner := db.ApplicationOwner{
				ApplicationOwnerApplicationID: applicationNew.Application_id,
				ApplicationOwnerUserID:        clusterUserDb.Clusteruser_id,
			}

			err = dbq.CreateApplicationOwner(ctx, &applicationOwner)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateApplication function since CreateApplication does not allow to insert custom "Created_on" field.
			err = dbq.GetApplicationById(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field more than waitTimeforRowDelete
			applicationNew.Created_on = time.Now().Add(time.Duration(-(waitTimeforRowDelete + 1)))
			err = dbq.UpdateApplication(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")
			cleanOrphanedEntriesfromTable_Application(ctx, dbq, k8sClient, true, log)

			By("Verify that application row is deleted from DB.")
			err = dbq.GetApplicationById(ctx, &applicationNew)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			By("Verify that applicationOwner row is deleted from DB.")
			err = dbq.GetApplicationOwnerByApplicationID(ctx, &applicationOwner)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

		})

		It("Should delete applicationState entry if application entry is deleted from DB", func() {
			defer dbq.CloseDatabase()

			By("Create application row without DTAM entry.")

			// Create dummy Application Spec to be saved in DB
			dummyApplicationSpec := fauxargocd.FauxApplication{
				FauxObjectMeta: fauxargocd.FauxObjectMeta{
					Namespace: "argocd",
				},
			}
			dummyApplicationSpecBytes, err := yaml.Marshal(dummyApplicationSpec)
			Expect(err).ToNot(HaveOccurred())

			applicationNew := db.Application{
				Application_id:          "test-app-" + string(uuid.NewUUID()),
				Name:                    "test-app-" + string(uuid.NewUUID()),
				Spec_field:              string(dummyApplicationSpecBytes),
				Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
				Managed_environment_id:  managedEnvironment.Managedenvironment_id,
			}
			err = dbq.CreateApplication(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			applicationState := db.ApplicationState{
				Applicationstate_application_id: applicationNew.Application_id,
				Health:                          "Healthy",
				Sync_Status:                     "Synced",
				ReconciledState:                 "Healthy",
			}
			err = dbq.CreateApplicationState(ctx, &applicationState)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateApplication function since CreateApplication does not allow to insert custom "Created_on" field.
			err = dbq.GetApplicationById(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field more than waitTimeforRowDelete
			applicationNew.Created_on = time.Now().Add(time.Duration(-(waitTimeforRowDelete + 1)))
			err = dbq.UpdateApplication(ctx, &applicationNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")
			cleanOrphanedEntriesfromTable_Application(ctx, dbq, k8sClient, true, log)

			By("Verify that application row entry is deleted from DB.")
			err = dbq.GetApplicationById(ctx, &applicationNew)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			By("Verify that applicationState row entry is deleted from DB.")
			err = dbq.GetApplicationStateById(ctx, &applicationState)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())
		})
	})

	Context("Testing cleanOrphanedEntriesfromTable function for RepositoryCredentials table entries.", func() {

		var log logr.Logger
		var ctx context.Context
		var dbq db.AllDatabaseQueries
		var k8sClient client.WithWatch
		var clusterUserDb *db.ClusterUser
		var gitopsEngineInstance *db.GitopsEngineInstance
		var apiCRToDatabaseMappingDb db.APICRToDatabaseMapping
		var gitopsRepositoryCredentialsDb db.RepositoryCredentials

		BeforeEach(func() {
			scheme,
				argocdNamespace,
				kubesystemNamespace,
				apiNamespace,
				err := tests.GenericTestSetup()
			Expect(err).ToNot(HaveOccurred())

			// Create fake client
			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
				Build()

			err = db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx = context.Background()
			log = logger.FromContext(ctx)
			dbq, err = db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())

			By("Create required DB entries.")

			_, _, _, gitopsEngineInstance, _, err = db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			// Create DB entry for ClusterUser
			clusterUserDb = &db.ClusterUser{
				Clusteruser_id: "test-repocred-user-id",
				User_name:      "test-repocred-user",
			}
			err = dbq.CreateClusterUser(ctx, clusterUserDb)
			Expect(err).ToNot(HaveOccurred())

			// Create DB entry for RepositoryCredentials
			gitopsRepositoryCredentialsDb = db.RepositoryCredentials{
				RepositoryCredentialsID: "test-repo-" + string(uuid.NewUUID()),
				UserID:                  clusterUserDb.Clusteruser_id,
				PrivateURL:              "https://test-private-url",
				AuthUsername:            "test-auth-username",
				AuthPassword:            "test-auth-password",
				AuthSSHKey:              "test-auth-ssh-key",
				SecretObj:               "test-secret-obj",
				EngineClusterID:         gitopsEngineInstance.Gitopsengineinstance_id,
			}
			err = dbq.CreateRepositoryCredentials(ctx, &gitopsRepositoryCredentialsDb)
			Expect(err).ToNot(HaveOccurred())

			// Create DB entry for APICRToDatabaseMapping
			apiCRToDatabaseMappingDb = db.APICRToDatabaseMapping{
				APIResourceType:      db.APICRToDatabaseMapping_ResourceType_GitOpsDeploymentRepositoryCredential,
				APIResourceUID:       "test-" + string(uuid.NewUUID()),
				APIResourceName:      "test-" + string(uuid.NewUUID()),
				APIResourceNamespace: "test-" + string(uuid.NewUUID()),
				NamespaceUID:         "test-" + string(uuid.NewUUID()),
				DBRelationType:       db.APICRToDatabaseMapping_DBRelationType_RepositoryCredential,
				DBRelationKey:        gitopsRepositoryCredentialsDb.RepositoryCredentialsID,
			}

			err = dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete RepositoryCredentials entry if its ACTDM entry is available.", func() {

			defer dbq.CloseDatabase()

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			By("Verify that no entry is deleted from DB.")

			_, err := dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDb.RepositoryCredentialsID)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should delete RepositoryCredentials entry if its ACTDM entry is not available.", func() {

			defer dbq.CloseDatabase()

			By("Create RepositoryCredentials row without DTAM entry.")

			gitopsRepositoryCredentialsDbNew := db.RepositoryCredentials{
				RepositoryCredentialsID: "test-repo-" + string(uuid.NewUUID()),
				UserID:                  clusterUserDb.Clusteruser_id,
				PrivateURL:              "https://test-private-url",
				AuthUsername:            "test-auth-username",
				AuthPassword:            "test-auth-password",
				AuthSSHKey:              "test-auth-ssh-key",
				SecretObj:               "test-secret-obj",
				EngineClusterID:         gitopsEngineInstance.Gitopsengineinstance_id,
			}
			err := dbq.CreateRepositoryCredentials(ctx, &gitopsRepositoryCredentialsDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateRepositoryCredentials function since CreateRepositoryCredentials does not allow to insert custom "Created_on" field.
			gitopsRepositoryCredentialsDbNew, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDbNew.RepositoryCredentialsID)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field more than waitTimeforRowDelete
			gitopsRepositoryCredentialsDbNew.Created_on = time.Now().Add(time.Duration(-(waitTimeforRowDelete + 1)))
			err = dbq.UpdateRepositoryCredentials(ctx, &gitopsRepositoryCredentialsDbNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			By("Verify that repository credentials row without DTAM entry is deleted from DB.")

			_, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDbNew.RepositoryCredentialsID)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			By("Verify that repository credentials row having DTAM entry is not deleted from DB.")

			_, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDb.RepositoryCredentialsID)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete RepositoryCredentials entry if its ACTDM entry is not available, but created time is less than wait time for deletion.", func() {
			defer dbq.CloseDatabase()

			By("Create application row without DTAM entry.")

			gitopsRepositoryCredentialsDbNew := db.RepositoryCredentials{
				RepositoryCredentialsID: "test-repo-" + string(uuid.NewUUID()),
				UserID:                  clusterUserDb.Clusteruser_id,
				PrivateURL:              "https://test-private-url",
				AuthUsername:            "test-auth-username",
				AuthPassword:            "test-auth-password",
				AuthSSHKey:              "test-auth-ssh-key",
				SecretObj:               "test-secret-obj",
				EngineClusterID:         gitopsEngineInstance.Gitopsengineinstance_id,
			}
			err := dbq.CreateRepositoryCredentials(ctx, &gitopsRepositoryCredentialsDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateApplication function since CreateApplication does not allow to insert custom "Created_on" field.
			gitopsRepositoryCredentialsDbNew, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDbNew.RepositoryCredentialsID)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field less than waitTimeforRowDelete
			gitopsRepositoryCredentialsDbNew.Created_on = time.Now().Add(time.Duration(-30) * time.Minute)
			err = dbq.UpdateRepositoryCredentials(ctx, &gitopsRepositoryCredentialsDbNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			By("Verify that no application rows are deleted from DB.")

			_, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDbNew.RepositoryCredentialsID)
			Expect(err).ToNot(HaveOccurred())

			_, err = dbq.GetRepositoryCredentialsByID(ctx, gitopsRepositoryCredentialsDb.RepositoryCredentialsID)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("Testing cleanOrphanedEntriesfromTable function for SyncOperation table entries.", func() {

		var log logr.Logger
		var ctx context.Context
		var dbq db.AllDatabaseQueries
		var k8sClient client.WithWatch
		var applicationDb *db.Application
		var syncOperationDb db.SyncOperation
		var apiCRToDatabaseMappingDb db.APICRToDatabaseMapping

		BeforeEach(func() {
			scheme,
				argocdNamespace,
				kubesystemNamespace,
				apiNamespace,
				err := tests.GenericTestSetup()
			Expect(err).ToNot(HaveOccurred())

			// Create fake client
			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
				Build()

			err = db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx = context.Background()
			log = logger.FromContext(ctx)
			dbq, err = db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())

			By("Create required DB entries.")

			_, managedEnvironment, _, gitopsEngineInstance, _, err := db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			// Create dummy Application Spec to be saved in DB
			dummyApplicationSpec := fauxargocd.FauxApplication{
				FauxObjectMeta: fauxargocd.FauxObjectMeta{
					Namespace: "argocd",
				},
			}
			dummyApplicationSpecBytes, err := yaml.Marshal(dummyApplicationSpec)
			Expect(err).ToNot(HaveOccurred())

			// Create DB entry for Application
			applicationDb = &db.Application{
				Application_id:          "test-app-" + string(uuid.NewUUID()),
				Name:                    "test-app-" + string(uuid.NewUUID()),
				Spec_field:              string(dummyApplicationSpecBytes),
				Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
				Managed_environment_id:  managedEnvironment.Managedenvironment_id,
			}
			err = dbq.CreateApplication(ctx, applicationDb)
			Expect(err).ToNot(HaveOccurred())

			// Create DB entry for SyncOperation
			syncOperationDb = db.SyncOperation{
				SyncOperation_id:    "test-op-" + string(uuid.NewUUID()),
				Application_id:      applicationDb.Application_id,
				DeploymentNameField: "test-depl-" + string(uuid.NewUUID()),
				Revision:            "Head",
				DesiredState:        "Terminated",
			}
			err = dbq.CreateSyncOperation(ctx, &syncOperationDb)
			Expect(err).ToNot(HaveOccurred())

			// Create DB entry for APICRToDatabaseMappingapiCRToDatabaseMapping
			apiCRToDatabaseMappingDb = db.APICRToDatabaseMapping{
				APIResourceType:      db.APICRToDatabaseMapping_ResourceType_GitOpsDeploymentSyncRun,
				APIResourceUID:       "test-" + string(uuid.NewUUID()),
				APIResourceName:      "test-" + string(uuid.NewUUID()),
				APIResourceNamespace: "test-" + string(uuid.NewUUID()),
				NamespaceUID:         "test-" + string(uuid.NewUUID()),
				DBRelationType:       db.APICRToDatabaseMapping_DBRelationType_SyncOperation,
				DBRelationKey:        syncOperationDb.SyncOperation_id,
			}

			err = dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete syncOperation entry if its ACTDM entry is available.", func() {

			defer dbq.CloseDatabase()

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			By("Verify that no entry is deleted from DB.")

			err := dbq.GetSyncOperationById(ctx, &syncOperationDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should delete syncOperation entry if its ACTDM entry is not available.", func() {

			defer dbq.CloseDatabase()

			By("Create RepositoryCredentials row without DTAM entry.")

			syncOperationDbNew := db.SyncOperation{
				SyncOperation_id:    "test-op-" + string(uuid.NewUUID()),
				Application_id:      applicationDb.Application_id,
				DeploymentNameField: "test-depl-" + string(uuid.NewUUID()),
				Revision:            "Head",
				DesiredState:        "Terminated",
			}
			err := dbq.CreateSyncOperation(ctx, &syncOperationDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateSyncOperation function since CreateSyncOperation does not allow to insert custom "Created_on" field.
			err = dbq.GetSyncOperationById(ctx, &syncOperationDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field more than waitTimeforRowDelete
			syncOperationDbNew.Created_on = time.Now().Add(time.Duration(-(waitTimeforRowDelete + 1)))
			err = dbq.UpdateSyncOperation(ctx, &syncOperationDbNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			By("Verify that SyncOperation row without DTAM entry is deleted from DB.")

			err = dbq.GetSyncOperationById(ctx, &syncOperationDbNew)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			By("Verify that SyncOperation row having DTAM entry is not deleted from DB.")

			err = dbq.GetSyncOperationById(ctx, &syncOperationDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete syncOperation entry if its ACTDM entry is not available, but created time is less than wait time for deletion.", func() {

			defer dbq.CloseDatabase()

			By("Create application row without DTAM entry.")

			syncOperationDbNew := db.SyncOperation{
				SyncOperation_id:    "test-op-" + string(uuid.NewUUID()),
				Application_id:      applicationDb.Application_id,
				DeploymentNameField: "test-depl-" + string(uuid.NewUUID()),
				Revision:            "Head",
				DesiredState:        "Terminated",
			}
			err := dbq.CreateSyncOperation(ctx, &syncOperationDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateSyncOperation function since CreateSyncOperation does not allow to insert custom "Created_on" field.
			err = dbq.GetSyncOperationById(ctx, &syncOperationDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field less than waitTimeforRowDelete
			syncOperationDbNew.Created_on = time.Now().Add(time.Duration(-30) * time.Minute)
			err = dbq.UpdateSyncOperation(ctx, &syncOperationDbNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			By("Verify that no application rows are deleted from DB.")

			err = dbq.GetSyncOperationById(ctx, &syncOperationDbNew)
			Expect(err).ToNot(HaveOccurred())

			err = dbq.GetSyncOperationById(ctx, &syncOperationDb)
			Expect(err).ToNot(HaveOccurred())
		})

	})

	Context("Testing cleanOrphanedEntriesfromTable function for ManagedEnvironment table entries.", func() {

		var log logr.Logger
		var ctx context.Context
		var dbq db.AllDatabaseQueries
		var k8sClient client.WithWatch
		var clusterCredentialsDb db.ClusterCredentials
		var managedEnvironmentDb db.ManagedEnvironment
		var apiCRToDatabaseMappingDb db.APICRToDatabaseMapping

		BeforeEach(func() {
			scheme,
				argocdNamespace,
				kubesystemNamespace,
				apiNamespace,
				err := tests.GenericTestSetup()
			Expect(err).ToNot(HaveOccurred())

			// Create fake client
			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
				Build()

			err = db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx = context.Background()
			log = logger.FromContext(ctx)
			dbq, err = db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())

			By("Create required DB entries.")

			// Create DB entry for ClusterCredentials
			clusterCredentialsDb = db.ClusterCredentials{
				Clustercredentials_cred_id:  "test-" + string(uuid.NewUUID()),
				Host:                        "host",
				Kube_config:                 "kube-config",
				Kube_config_context:         "kube-config-context",
				Serviceaccount_bearer_token: "serviceaccount_bearer_token",
				Serviceaccount_ns:           "Serviceaccount_ns",
			}
			err = dbq.CreateClusterCredentials(ctx, &clusterCredentialsDb)
			Expect(err).ToNot(HaveOccurred())

			// Create DB entry for ManagedEnvironment
			managedEnvironmentDb = db.ManagedEnvironment{
				Managedenvironment_id: "test-" + string(uuid.NewUUID()),
				Clustercredentials_id: clusterCredentialsDb.Clustercredentials_cred_id,
				Name:                  "test-" + string(uuid.NewUUID()),
			}
			err = dbq.CreateManagedEnvironment(ctx, &managedEnvironmentDb)
			Expect(err).ToNot(HaveOccurred())

			// Create DB entry for APICRToDatabaseMapping
			apiCRToDatabaseMappingDb = db.APICRToDatabaseMapping{
				APIResourceType:      db.APICRToDatabaseMapping_ResourceType_GitOpsDeploymentManagedEnvironment,
				APIResourceUID:       string(uuid.NewUUID()),
				APIResourceName:      managedEnvironmentDb.Name,
				APIResourceNamespace: "test-" + string(uuid.NewUUID()),
				NamespaceUID:         "test-" + string(uuid.NewUUID()),
				DBRelationType:       db.APICRToDatabaseMapping_DBRelationType_ManagedEnvironment,
				DBRelationKey:        managedEnvironmentDb.Managedenvironment_id,
			}

			err = dbq.CreateAPICRToDatabaseMapping(ctx, &apiCRToDatabaseMappingDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete ManagedEnvironment entry if its ACTDM entry is available.", func() {

			defer dbq.CloseDatabase()

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, MockSRLK8sClientFactory{fakeClient: k8sClient}, true, log)

			By("Verify that no entry is deleted from DB.")

			err := dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should delete ManagedEnvironment entry if its ACTDM entry is not available", func() {

			defer dbq.CloseDatabase()

			By("Create ManagedEnvironment row without ACTDM entry.")

			managedEnvironmentDbNew := db.ManagedEnvironment{
				Managedenvironment_id: "test-" + string(uuid.NewUUID()),
				Clustercredentials_id: clusterCredentialsDb.Clustercredentials_cred_id,
				Name:                  "test-" + string(uuid.NewUUID()),
			}
			err := dbq.CreateManagedEnvironment(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateManagedEnvironment function since CreateManagedEnvironment does not allow to insert custom "Created_on" field.
			err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field more than waitTimeforRowDelete
			managedEnvironmentDbNew.Created_on = time.Now().Add(time.Duration(-(waitTimeforRowDelete + 1)))

			err = dbq.UpdateManagedEnvironment(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			By("Verify that SyncOperation row without DTAM entry is deleted from DB.")

			err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDbNew)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			By("Verify that SyncOperation row having DTAM entry is not deleted from DB.")

			err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete ManagedEnvironment entry if its ACTDM entry is not available, but created time is less than wait time for deletion.", func() {

			defer dbq.CloseDatabase()

			By("Create ManagedEnvironment row without ACTDM entry.")

			managedEnvironmentDbNew := db.ManagedEnvironment{
				Managedenvironment_id: "test-" + string(uuid.NewUUID()),
				Clustercredentials_id: clusterCredentialsDb.Clustercredentials_cred_id,
				Name:                  "test-" + string(uuid.NewUUID()),
			}
			err := dbq.CreateManagedEnvironment(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" field using UpdateManagedEnvironment function since CreateManagedEnvironment does not allow to insert custom "Created_on" field.
			err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field less than waitTimeforRowDelete
			managedEnvironmentDbNew.Created_on = time.Now().Add(time.Duration(-30) * time.Minute)
			err = dbq.UpdateManagedEnvironment(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			By("Verify that no application rows are deleted from DB.")

			err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred())

			err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete ManagedEnvironment entry if there exists an entry in the KubernetesToDBResourceMapping table that points to that ME", func() {

			defer dbq.CloseDatabase()

			By("Create ManagedEnvironment row without an ACTDM entry.")

			managedEnvironmentDbNew := db.ManagedEnvironment{
				Managedenvironment_id: "test-" + string(uuid.NewUUID()),
				Clustercredentials_id: clusterCredentialsDb.Clustercredentials_cred_id,
				Name:                  "test-" + string(uuid.NewUUID()),
			}
			err := dbq.CreateManagedEnvironment(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred())

			By("updating created_on, so it does not block the ManagedEnvironment from being deleted")

			// Set "Created_on" field to > waitTimeForRowDelete
			managedEnvironmentDbNew.Created_on = time.Now().Add(-1 * (waitTimeforRowDelete + 1*time.Second))
			err = dbq.UpdateManagedEnvironment(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred())

			By("creating a KubernetesDBToResourceMapping table referencing the ManagedEnvironment")
			kubernetesDBToResourceMapping := db.KubernetesToDBResourceMapping{
				KubernetesResourceType: db.K8sToDBMapping_Namespace,
				KubernetesResourceUID:  "test-" + string(uuid.NewUUID()),
				DBRelationType:         db.K8sToDBMapping_ManagedEnvironment,
				DBRelationKey:          managedEnvironmentDbNew.Managedenvironment_id,
			}
			err = dbq.CreateKubernetesResourceToDBResourceMapping(ctx, &kubernetesDBToResourceMapping)
			Expect(err).ToNot(HaveOccurred())

			By("calling the function under test")
			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDbNew)
			Expect(err).ToNot(HaveOccurred(), "the ManagedEnvironment should exist: it should not be deleted")

			By("deleting the KubernetesToDBResourceMapping")
			rowsDeleted, err := dbq.DeleteKubernetesResourceToDBResourceMapping(ctx, &kubernetesDBToResourceMapping)
			Expect(err).ToNot(HaveOccurred())
			Expect(rowsDeleted).To(Equal(1))

			By("calling the function under test")
			cleanOrphanedEntriesfromTable(ctx, dbq, k8sClient, nil, true, log)

			err = dbq.GetManagedEnvironmentById(ctx, &managedEnvironmentDbNew)
			Expect(err).To(HaveOccurred(), "the ManagedEnvironment should no longer exist, after the KubernetesToDBResourceMapping was deleted")
			Expect(db.IsResultNotFoundError(err)).To(BeTrue(), "the ManagedEnvironment should no longer exist, after the KubernetesToDBResourceMapping was deleted")

		})

	})

	Context("Testing cleanOrphanedEntriesfromTable_Operation function for Operation table entries.", func() {

		var log logr.Logger
		var ctx context.Context
		var dbq db.AllDatabaseQueries
		var k8sClient client.WithWatch
		var clusterAccess *db.ClusterAccess
		var gitopsEngineInstance *db.GitopsEngineInstance

		BeforeEach(func() {
			scheme,
				argocdNamespace,
				kubesystemNamespace,
				apiNamespace,
				err := tests.GenericTestSetup()
			Expect(err).ToNot(HaveOccurred())

			// Create fake client
			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
				Build()

			err = db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx = context.Background()
			log = logger.FromContext(ctx)
			dbq, err = db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())

			_, _, _, gitopsEngineInstance, clusterAccess, err = db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

		})

		It("Should not delete operation entry, if Operation is completed, but its creation time is less than 24 hours.", func() {

			By("create an operation entry in DB")

			operationDb := db.Operation{
				Operation_id:            "test-operation-1",
				Instance_id:             gitopsEngineInstance.Gitopsengineinstance_id,
				Resource_id:             "test-fake-resource-id",
				Resource_type:           "GitopsEngineInstance",
				State:                   db.OperationState_Waiting,
				Operation_owner_user_id: clusterAccess.Clusteraccess_user_id,
				GC_expiration_time:      2,
				Last_state_update:       time.Now(),
			}
			err := dbq.CreateOperation(ctx, &operationDb, operationDb.Operation_owner_user_id)
			Expect(err).ToNot(HaveOccurred())

			// Change "State" and "Created_on" fields using UpdateOperation function since CreateOperation does not allow to insert custom "Created_on" and "State" field.
			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			// Set "State" field to "Completed"
			operationDb.State = db.OperationState_Completed

			// Set "Created_on" field to more than 23 Hours
			operationDb.Created_on = time.Now().Add(time.Duration(-23) * time.Hour)

			err = dbq.UpdateOperation(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_Operation(ctx, dbq, k8sClient, true, log)

			By("Verify that no entry is deleted from DB.")

			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete operation entry, if Operation state is In-Progress, but its creation time is more than 24 hours.", func() {

			By("create an operation entry in DB")

			operationDb := db.Operation{
				Operation_id:            "test-operation-1",
				Instance_id:             gitopsEngineInstance.Gitopsengineinstance_id,
				Resource_id:             "test-fake-resource-id",
				Resource_type:           "GitopsEngineInstance",
				State:                   db.OperationState_Waiting,
				Operation_owner_user_id: clusterAccess.Clusteraccess_user_id,
				GC_expiration_time:      2,
				Last_state_update:       time.Now(),
			}
			err := dbq.CreateOperation(ctx, &operationDb, operationDb.Operation_owner_user_id)
			Expect(err).ToNot(HaveOccurred())

			// Change "State" and "Created_on" fields using UpdateOperation function since CreateOperation does not allow to insert custom "Created_on" and "State" field.
			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			// Set "State" field to "In_Progress"
			operationDb.State = db.OperationState_In_Progress

			// Set "Created_on" field to more than 23 Hours
			operationDb.Created_on = time.Now().Add(time.Duration(-25) * time.Hour)

			err = dbq.UpdateOperation(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_Operation(ctx, dbq, k8sClient, true, log)

			By("Verify that no entry is deleted from DB.")

			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete operation entry, if Operation state is Waiting and its creation time is more than 24 hours.", func() {

			By("create an operation entry in DB")

			operationDb := db.Operation{
				Operation_id:            "test-operation-1",
				Instance_id:             gitopsEngineInstance.Gitopsengineinstance_id,
				Resource_id:             "test-fake-resource-id",
				Resource_type:           "GitopsEngineInstance",
				State:                   db.OperationState_Waiting,
				Operation_owner_user_id: clusterAccess.Clusteraccess_user_id,
				GC_expiration_time:      2,
				Last_state_update:       time.Now(),
			}
			err := dbq.CreateOperation(ctx, &operationDb, operationDb.Operation_owner_user_id)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" fields using UpdateOperation function since CreateOperation does not allow to insert custom "Created_on" field.
			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field to more than 23 Hours
			operationDb.Created_on = time.Now().Add(time.Duration(-25) * time.Hour)

			err = dbq.UpdateOperation(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_Operation(ctx, dbq, k8sClient, true, log)

			By("Verify that no entry is deleted from DB.")

			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should delete operation entry, if Operation state is Completed and its creation time is more than 24 Hours.", func() {

			By("create an operation entry in DB")

			operationDb := db.Operation{
				Operation_id:            "test-operation-1",
				Instance_id:             gitopsEngineInstance.Gitopsengineinstance_id,
				Resource_id:             "test-fake-resource-id",
				Resource_type:           "GitopsEngineInstance",
				State:                   db.OperationState_Completed,
				Operation_owner_user_id: clusterAccess.Clusteraccess_user_id,
				GC_expiration_time:      2,
				Last_state_update:       time.Now(),
			}
			err := dbq.CreateOperation(ctx, &operationDb, operationDb.Operation_owner_user_id)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" and "State" fields using UpdateApplication function since CreateApplication does not allow to insert custom "Created_on" and "State" field.
			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			// Set "State" field to "Completed"
			operationDb.State = db.OperationState_Completed

			// Set "Created_on" field to more than 24 Hours
			operationDb.Created_on = time.Now().Add(time.Duration(-25) * time.Hour)

			err = dbq.UpdateOperation(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_Operation(ctx, dbq, k8sClient, true, log)

			By("Verify that entry is deleted from DB.")

			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())
		})

		It("Should recreate operation CR, if it is missing in cluster also the 'State' is given as 'Waiting' in DB and its creation time is more than 1 hour.", func() {

			By("create an operation entry in DB")

			operationDb := db.Operation{
				Operation_id:            "test-operation-1",
				Instance_id:             gitopsEngineInstance.Gitopsengineinstance_id,
				Resource_id:             "test-fake-resource-id",
				Resource_type:           "GitopsEngineInstance",
				State:                   db.OperationState_Waiting,
				Operation_owner_user_id: clusterAccess.Clusteraccess_user_id,
				GC_expiration_time:      2,
				Last_state_update:       time.Now(),
			}
			err := dbq.CreateOperation(ctx, &operationDb, operationDb.Operation_owner_user_id)
			Expect(err).ToNot(HaveOccurred())

			// Change "Created_on" fields using UpdateApplication function since CreateApplication does not allow to insert custom "Created_on" field.
			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field to more than 1 Hours
			operationDb.Created_on = time.Now().Add(time.Duration(-1) * time.Hour)

			err = dbq.UpdateOperation(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_Operation(ctx, dbq, k8sClient, true, log)

			By("Verify that no entry is deleted from DB.")

			err = dbq.GetOperationById(ctx, &operationDb)
			Expect(err).ToNot(HaveOccurred())

			By("Verify that Operation CR is created in Cluster.")

			operationCR := &managedgitopsv1alpha1.Operation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sharedoperations.GenerateOperationCRName(operationDb),
					Namespace: gitopsEngineInstance.Namespace_name,
				},
			}

			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(operationCR), operationCR)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("Testing cleanOrphanedEntriesfromTable_ClusterUser function for ClusterUser table entries.", func() {

		var log logr.Logger
		var ctx context.Context
		var user db.ClusterUser
		var dbq db.AllDatabaseQueries
		var k8sClient client.WithWatch

		BeforeEach(func() {
			scheme,
				argocdNamespace,
				kubesystemNamespace,
				apiNamespace,
				err := tests.GenericTestSetup()
			Expect(err).ToNot(HaveOccurred())

			// Create fake client
			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
				Build()

			err = db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx = context.Background()
			log = logger.FromContext(ctx)
			dbq, err = db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())

			user = db.ClusterUser{
				Clusteruser_id: "test-id-" + string(uuid.NewUUID()),
				User_name:      "test-name-" + string(uuid.NewUUID()),
			}

		})

		It("Should delete ClusterUser, if it is not used in any other table and it's created time is more than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			// Set "Created_on" field to > waitTimeForRowDelete
			user.Created_on = time.Now().Add(-1 * (waitTimeforRowDelete + 1*time.Second))

			By("Create Cluster user.")

			err := dbq.CreateClusterUser(ctx, &user)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterUser(ctx, dbq, k8sClient, true, log)

			By("Verify that ClusterUser entry is deleted from DB.")

			err = dbq.GetClusterUserByUsername(ctx, &user)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())
		})

		It("Should not delete ClusterUser, if it is not used in any other table but it's created time is less than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			By("Create Cluster user.")

			err := dbq.CreateClusterUser(ctx, &user)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterUser(ctx, dbq, k8sClient, true, log)

			By("Verify that ClusterUser entry is not deleted from DB.")

			err = dbq.GetClusterUserByUsername(ctx, &user)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete ClusterUser, if it is used in ClusterAccess entry even it's created time is more than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			// Set "Created_on" field to > waitTimeForRowDelete
			user.Created_on = time.Now().Add(-1 * (waitTimeforRowDelete + 1*time.Second))

			By("Create Cluster user.")

			err := dbq.CreateClusterUser(ctx, &user)
			Expect(err).ToNot(HaveOccurred())

			By("Create required DB entries.")

			_, managedEnvironment, _, gitopsEngineInstance, _, err := db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			clusterAccess := db.ClusterAccess{
				Clusteraccess_user_id:                   user.Clusteruser_id,
				Clusteraccess_managed_environment_id:    managedEnvironment.Managedenvironment_id,
				Clusteraccess_gitops_engine_instance_id: gitopsEngineInstance.Gitopsengineinstance_id,
			}

			err = dbq.CreateClusterAccess(ctx, &clusterAccess)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterUser(ctx, dbq, k8sClient, true, log)

			By("Verify that ClusterUser entry is not deleted from DB.")

			err = dbq.GetClusterUserByUsername(ctx, &user)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete ClusterUser, if it is used in RepositoryCredentials entry, it's created time is more than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			// Set "Created_on" field to > waitTimeForRowDelete
			user.Created_on = time.Now().Add(-1 * (waitTimeforRowDelete + 1*time.Second))

			By("Create Cluster user.")

			err := dbq.CreateClusterUser(ctx, &user)
			Expect(err).ToNot(HaveOccurred())

			By("Create required DB entries.")

			_, _, _, gitopsEngineInstance, _, err := db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			repositoryCredentials := db.RepositoryCredentials{
				RepositoryCredentialsID: "test-repo-cred-id",
				UserID:                  user.Clusteruser_id,
				PrivateURL:              "https://test-private-url",
				AuthUsername:            "test-auth-username",
				AuthPassword:            "test-auth-password",
				AuthSSHKey:              "test-auth-ssh-key",
				SecretObj:               "test-secret-obj",
				EngineClusterID:         gitopsEngineInstance.Gitopsengineinstance_id, // constrain 'fk_gitopsengineinstance_id'
			}

			err = dbq.CreateRepositoryCredentials(ctx, &repositoryCredentials)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterUser(ctx, dbq, k8sClient, true, log)

			By("Verify that ClusterUser entry is not deleted from DB.")

			err = dbq.GetClusterUserByUsername(ctx, &user)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete ClusterUser, if it is used in Operation entry, even it's created time is more than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			// Set "Created_on" field to > waitTimeForRowDelete
			user.Created_on = time.Now().Add(-1 * (waitTimeforRowDelete + 1*time.Second))

			By("Create Cluster user.")

			err := dbq.CreateClusterUser(ctx, &user)
			Expect(err).ToNot(HaveOccurred())

			By("Create required DB entries.")

			_, _, _, gitopsEngineInstance, _, err := db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			operation := db.Operation{
				Operation_id:            "test-operation-1",
				Instance_id:             gitopsEngineInstance.Gitopsengineinstance_id,
				Resource_id:             "test-fake-resource-id",
				Resource_type:           "GitopsEngineInstance",
				State:                   db.OperationState_Waiting,
				Operation_owner_user_id: user.Clusteruser_id,
			}

			err = dbq.CreateOperation(ctx, &operation, operation.Operation_owner_user_id)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterUser(ctx, dbq, k8sClient, true, log)

			By("Verify that ClusterUser entry is not deleted from DB.")

			err = dbq.GetClusterUserByUsername(ctx, &user)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete 'Special User', even if it is not used in any other table and it's created time is more than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			// Delete 'Special User' if it is already present, since 'SetupForTestingDBGinkgo' can not delete it
			_, err := dbq.DeleteClusterUserById(ctx, db.SpecialClusterUserName)
			Expect(err).ToNot(HaveOccurred())

			// Set "Created_on" field to > waitTimeForRowDelete
			user.Created_on = time.Now().Add(-1 * (waitTimeforRowDelete + 1*time.Second))

			// Set Special User name
			user.User_name = db.SpecialClusterUserName

			By("Create Cluster user.")

			err = dbq.CreateClusterUser(ctx, &user)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterUser(ctx, dbq, k8sClient, true, log)

			By("Verify that ClusterUser entry is deleted from DB.")

			err = dbq.GetClusterUserByUsername(ctx, &user)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())
		})
	})

	Context("Testing cleanOrphanedEntriesfromTable_ClusterCredential function for ClusterCredential table entries.", func() {

		var log logr.Logger
		var ctx context.Context
		var dbq db.AllDatabaseQueries
		var k8sClient client.WithWatch
		var clusterCreds db.ClusterCredentials

		BeforeEach(func() {
			scheme,
				argocdNamespace,
				kubesystemNamespace,
				apiNamespace,
				err := tests.GenericTestSetup()
			Expect(err).ToNot(HaveOccurred())

			// Create fake client
			k8sClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(apiNamespace, argocdNamespace, kubesystemNamespace).
				Build()

			err = db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx = context.Background()
			log = logger.FromContext(ctx)
			dbq, err = db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())

			clusterCreds = db.ClusterCredentials{
				Clustercredentials_cred_id:  "test-" + string(uuid.NewUUID()),
				Host:                        "test-host",
				Kube_config:                 "test-kube_config",
				Kube_config_context:         "test-kube_config_context",
				Serviceaccount_bearer_token: "test-serviceaccount_bearer_token",
				Serviceaccount_ns:           "test-serviceaccount_ns",
			}
		})

		It("Should delete ClusterCredentials if it is not used in any other table and it's created time is more than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			// Set "Created_on" field to > waitTimeForRowDelete
			clusterCreds.Created_on = time.Now().Add(-1 * (waitTimeforRowDelete + 1*time.Second))

			By("Create Cluster Credential.")

			err := dbq.CreateClusterCredentials(ctx, &clusterCreds)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterCredential(ctx, dbq, k8sClient, true, log)

			By("Verify that Cluster Credential entry is deleted from DB.")

			err = dbq.GetClusterCredentialsById(ctx, &clusterCreds)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())
		})

		It("Should not delete ClusterCredentials if it is not used in any other table but it's created time is less than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			By("Create Cluster Credential.")

			err := dbq.CreateClusterCredentials(ctx, &clusterCreds)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterCredential(ctx, dbq, k8sClient, true, log)

			By("Verify that Cluster Credential entry is not deleted from DB.")

			err = dbq.GetClusterCredentialsById(ctx, &clusterCreds)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete ClusterCredentials if it is used in ManagedEnvironment entry, even it's created time is more than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			// Set "Created_on" field to > waitTimeForRowDelete
			clusterCreds.Created_on = time.Now().Add(-1 * (waitTimeforRowDelete + 1*time.Second))

			By("Create ClusterCredentials.")

			err := dbq.CreateClusterCredentials(ctx, &clusterCreds)
			Expect(err).ToNot(HaveOccurred())

			By("Create ManagedEnvironment.")

			managedEnvironment := db.ManagedEnvironment{
				Managedenvironment_id: "test-managed-env-3",
				Clustercredentials_id: clusterCreds.Clustercredentials_cred_id,
				Name:                  "my env101",
			}

			err = dbq.CreateManagedEnvironment(ctx, &managedEnvironment)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterCredential(ctx, dbq, k8sClient, true, log)

			By("Verify that Cluster Credential entry is not deleted from DB.")

			err = dbq.GetClusterCredentialsById(ctx, &clusterCreds)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not delete ClusterCredentials if it is used in RepositoryCredentials entry, even it's created time is more than 'waitTimeforRowDelete'.", func() {

			defer dbq.CloseDatabase()

			// Set "Created_on" field to > waitTimeForRowDelete
			clusterCreds.Created_on = time.Now().Add(-1 * (waitTimeforRowDelete + 1*time.Second))

			By("Create ClusterCredentials.")

			err := dbq.CreateClusterCredentials(ctx, &clusterCreds)
			Expect(err).ToNot(HaveOccurred())

			By("Create GitopsEngineCluster.")

			gitopsEngineCluster := db.GitopsEngineCluster{
				Gitopsenginecluster_id: "test-fake-cluster-1",
				Clustercredentials_id:  clusterCreds.Clustercredentials_cred_id,
			}

			err = dbq.CreateGitopsEngineCluster(ctx, &gitopsEngineCluster)
			Expect(err).ToNot(HaveOccurred())

			By("Call clean-up function.")

			cleanOrphanedEntriesfromTable_ClusterCredential(ctx, dbq, k8sClient, true, log)

			By("Verify that Cluster Credential entry is not deleted from DB.")

			err = dbq.GetClusterCredentialsById(ctx, &clusterCreds)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

type MockSRLK8sClientFactory struct {
	fakeClient client.Client
}

func (f MockSRLK8sClientFactory) BuildK8sClient(restConfig *rest.Config) (client.Client, error) {
	return f.fakeClient, nil
}

func (f MockSRLK8sClientFactory) GetK8sClientForGitOpsEngineInstance(ctx context.Context, gitopsEngineInstance *db.GitopsEngineInstance) (client.Client, error) {
	return f.fakeClient, nil
}

func (f MockSRLK8sClientFactory) GetK8sClientForServiceWorkspace() (client.Client, error) {
	return f.fakeClient, nil
}
