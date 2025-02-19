package db_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	db "github.com/redhat-appstudio/managed-gitops/backend-shared/db"
)

var _ = Describe("Types Test", func() {
	Context("Tests all the functions for types.go", func() {

		var testClusterUser = &db.ClusterUser{
			Clusteruser_id: "test-user",
			User_name:      "test-user",
		}

		It("Should execute select on all the fields of the database.", func() {

			err := db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx := context.Background()

			dbq, err := db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())
			defer dbq.CloseDatabase()

			var applicationStates []db.ApplicationState
			err = dbq.UnsafeListAllApplicationStates(ctx, &applicationStates)
			Expect(err).ToNot(HaveOccurred())

			var applications []db.Application
			err = dbq.UnsafeListAllApplications(ctx, &applications)
			Expect(err).ToNot(HaveOccurred())

			var clusterAccess []db.ClusterAccess
			err = dbq.UnsafeListAllClusterAccess(ctx, &clusterAccess)
			Expect(err).ToNot(HaveOccurred())

			var clusterCredentials []db.ClusterCredentials
			err = dbq.UnsafeListAllClusterCredentials(ctx, &clusterCredentials)
			Expect(err).ToNot(HaveOccurred())

			var clusterUsers []db.ClusterUser
			err = dbq.UnsafeListAllClusterUsers(ctx, &clusterUsers)
			Expect(err).ToNot(HaveOccurred())

			var engineClusters []db.GitopsEngineCluster
			err = dbq.UnsafeListAllGitopsEngineClusters(ctx, &engineClusters)
			Expect(err).ToNot(HaveOccurred())

			var engineInstances []db.GitopsEngineInstance
			err = dbq.UnsafeListAllGitopsEngineInstances(ctx, &engineInstances)
			Expect(err).ToNot(HaveOccurred())

			var managedEnvironments []db.ManagedEnvironment
			err = dbq.UnsafeListAllManagedEnvironments(ctx, &managedEnvironments)
			Expect(err).ToNot(HaveOccurred())

			var operations []db.Operation
			err = dbq.UnsafeListAllOperations(ctx, &operations)
			Expect(err).ToNot(HaveOccurred())

			var appProjectRepository []db.AppProjectRepository
			err = dbq.UnsafeListAllAppProjectRepositories(ctx, &appProjectRepository)
			Expect(err).ToNot(HaveOccurred())

			var appProjectManagedEnv []db.AppProjectManagedEnvironment
			err = dbq.UnsafeListAllAppProjectManagedEnvironments(ctx, &appProjectManagedEnv)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should CheckedCreate and CheckedDelete an application", func() {

			err := db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx := context.Background()

			dbq, err := db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())
			defer dbq.CloseDatabase()

			_, managedEnvironment, _, gitopsEngineInstance, clusterAccess, err := db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			application := &db.Application{
				Application_id:          "test-my-application",
				Name:                    "my-application",
				Spec_field:              "{}",
				Engine_instance_inst_id: gitopsEngineInstance.Gitopsengineinstance_id,
				Managed_environment_id:  managedEnvironment.Managedenvironment_id,
			}

			err = dbq.CheckedCreateApplication(ctx, application, clusterAccess.Clusteraccess_user_id)
			Expect(err).ToNot(HaveOccurred())

			retrievedApplication := db.Application{Application_id: application.Application_id}

			err = dbq.GetApplicationById(ctx, &retrievedApplication)
			Expect(err).ToNot(HaveOccurred())
			Expect(application.Application_id).Should(Equal(retrievedApplication.Application_id))

			rowsAffected, err := dbq.CheckedDeleteApplicationById(ctx, application.Application_id, clusterAccess.Clusteraccess_user_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(rowsAffected).Should(Equal(1))

			retrievedApplication = db.Application{Application_id: application.Application_id}
			err = dbq.GetApplicationById(ctx, &retrievedApplication)
			Expect(true).To(Equal(db.IsResultNotFoundError(err)))

		})

		It("Should test deploymenttoapplication mapping", func() {

			err := db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx := context.Background()

			dbq, err := db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())
			defer dbq.CloseDatabase()

			mapping := db.DeploymentToApplicationMapping{}
			err = dbq.CheckedGetDeploymentToApplicationMappingByDeplId(ctx, &mapping, "")
			fmt.Println(err, mapping)

		})

		It("Should test GitopsEngineInstance and GitOpsEngineCluster", func() {

			err := db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx := context.Background()

			dbq, err := db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())
			defer dbq.CloseDatabase()

			_, _, gitopsEngineCluster, gitopsEngineInstance, clusterAccess, err := db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			retrievedGitopsEngineCluster := &db.GitopsEngineCluster{Gitopsenginecluster_id: gitopsEngineCluster.Gitopsenginecluster_id}
			err = dbq.CheckedGetGitopsEngineClusterById(ctx, retrievedGitopsEngineCluster, testClusterUser.Clusteruser_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(&gitopsEngineCluster).Should(Equal(&retrievedGitopsEngineCluster))

			rowsAffected, err := dbq.DeleteClusterAccessById(ctx, clusterAccess.Clusteraccess_user_id, clusterAccess.Clusteraccess_managed_environment_id, clusterAccess.Clusteraccess_gitops_engine_instance_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(rowsAffected).Should(Equal(1))

			rowsAffected, err = dbq.DeleteGitopsEngineInstanceById(ctx, gitopsEngineInstance.Gitopsengineinstance_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(rowsAffected).Should(Equal(1))

			gitopsEngineInstance = &db.GitopsEngineInstance{Gitopsengineinstance_id: gitopsEngineInstance.Gitopsengineinstance_id}
			err = dbq.CheckedGetGitopsEngineInstanceById(ctx, gitopsEngineInstance, testClusterUser.Clusteruser_id)
			Expect(err).To(HaveOccurred())
			Expect(true).To(Equal(db.IsResultNotFoundError(err)))

			rowsAffected, err = dbq.DeleteGitopsEngineClusterById(ctx, gitopsEngineCluster.Gitopsenginecluster_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(rowsAffected).Should(Equal(1))

			retrievedGitopsEngineCluster = &db.GitopsEngineCluster{Gitopsenginecluster_id: gitopsEngineCluster.Gitopsenginecluster_id}
			err = dbq.CheckedGetGitopsEngineClusterById(ctx, retrievedGitopsEngineCluster, testClusterUser.Clusteruser_id)
			Expect(err).To(HaveOccurred())
			Expect(true).To(Equal(db.IsResultNotFoundError(err)))
		})

		It("Should test ManagedEnvironment", func() {

			err := db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx := context.Background()

			dbq, err := db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())
			defer dbq.CloseDatabase()

			_, managedEnvironment, _, _, clusterAccess, err := db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			result := &db.ManagedEnvironment{Managedenvironment_id: managedEnvironment.Managedenvironment_id}

			err = dbq.CheckedGetManagedEnvironmentById(ctx, result, testClusterUser.Clusteruser_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(managedEnvironment.Created_on.After(time.Now().Add(time.Minute*-5))).To(BeTrue(), "Created on should be within the last 5 minutes")
			managedEnvironment.Created_on = result.Created_on
			Expect(managedEnvironment).Should(Equal(result))

			result = &db.ManagedEnvironment{Managedenvironment_id: managedEnvironment.Managedenvironment_id}
			err = dbq.CheckedGetManagedEnvironmentById(ctx, result, "another-user-test")
			Expect(err).To(HaveOccurred())
			// deleting from another user should fail
			Expect(true).To(Equal(db.IsResultNotFoundError(err)))

			rowsAffected, err := dbq.DeleteClusterAccessById(ctx, clusterAccess.Clusteraccess_user_id, clusterAccess.Clusteraccess_managed_environment_id, clusterAccess.Clusteraccess_gitops_engine_instance_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(rowsAffected).Should(Equal(1))

			rowsAffected, err = dbq.DeleteManagedEnvironmentById(ctx, managedEnvironment.Managedenvironment_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(rowsAffected).Should(Equal(1))

			result = &db.ManagedEnvironment{Managedenvironment_id: managedEnvironment.Managedenvironment_id}
			err = dbq.CheckedGetManagedEnvironmentById(ctx, result, testClusterUser.Clusteruser_id)
			Expect(err).To(HaveOccurred())
			Expect(true).To(Equal(db.IsResultNotFoundError(err)))

		})

		It("Should test Operations", func() {

			err := db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx := context.Background()

			dbq, err := db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())
			defer dbq.CloseDatabase()

			_, _, _, gitopsEngineInstance, _, err := db.CreateSampleData(dbq)
			Expect(err).ToNot(HaveOccurred())

			operation := &db.Operation{
				Operation_id:            "test-operation",
				Instance_id:             gitopsEngineInstance.Gitopsengineinstance_id,
				Resource_id:             "fake resource id",
				Resource_type:           "GitopsEngineInstance",
				State:                   db.OperationState_Waiting,
				Operation_owner_user_id: testClusterUser.Clusteruser_id,
			}

			err = dbq.CreateOperation(ctx, operation, operation.Operation_owner_user_id)
			Expect(err).ToNot(HaveOccurred())

			result := db.Operation{Operation_id: operation.Operation_id}
			err = dbq.CheckedGetOperationById(ctx, &result, operation.Operation_owner_user_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(operation.Operation_id).Should(Equal(result.Operation_id))

			result = db.Operation{Operation_id: operation.Operation_id}
			err = dbq.CheckedGetOperationById(ctx, &result, "another-user-test")
			Expect(err).To(HaveOccurred())
			Expect(true).To(Equal(db.IsResultNotFoundError(err)))

			rowsAffected, _ := dbq.CheckedDeleteOperationById(ctx, operation.Operation_id, "another-user")
			Expect(rowsAffected).Should(Equal(0))

			rowsAffected, err = dbq.CheckedDeleteOperationById(ctx, operation.Operation_id, operation.Operation_owner_user_id)
			Expect(rowsAffected).Should(Equal(1))
			Expect(err).ToNot(HaveOccurred())
		})
		It("Should test ClusterUser", func() {

			err := db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx := context.Background()

			dbq, err := db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())
			defer dbq.CloseDatabase()

			clusterUser := db.ClusterUser{
				Clusteruser_id: "test-my-cluster-user-2",
				User_name:      "cluster-mccluster",
			}
			err = dbq.CreateClusterUser(ctx, &clusterUser)
			Expect(err).ToNot(HaveOccurred())

			retrievedClusterUser := db.ClusterUser{Clusteruser_id: clusterUser.Clusteruser_id}
			err = dbq.GetClusterUserById(ctx, &retrievedClusterUser)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterUser.User_name).Should(Equal(retrievedClusterUser.User_name))

			rowsAffected, err := dbq.DeleteClusterUserById(ctx, clusterUser.Clusteruser_id)
			Expect(rowsAffected).Should(Equal(1))
			Expect(err).ToNot(HaveOccurred())

			retrievedClusterUser = db.ClusterUser{Clusteruser_id: clusterUser.Clusteruser_id}
			err = dbq.GetClusterUserById(ctx, &retrievedClusterUser)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())

			retrievedClusterUser = db.ClusterUser{Clusteruser_id: "does-not-exist"}
			err = dbq.GetClusterUserById(ctx, &retrievedClusterUser)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())
		})

		It("Should test ClusterCredentials", func() {

			err := db.SetupForTestingDBGinkgo()
			Expect(err).ToNot(HaveOccurred())

			ctx := context.Background()

			dbq, err := db.NewUnsafePostgresDBQueries(true, true)
			Expect(err).ToNot(HaveOccurred())
			defer dbq.CloseDatabase()

			clusterCredentials := db.ClusterCredentials{
				Clustercredentials_cred_id:  "test-cluster-creds-test",
				Host:                        "host",
				Kube_config:                 "kube-config",
				Kube_config_context:         "kube-config-context",
				Serviceaccount_bearer_token: "serviceaccount_bearer_token",
				Serviceaccount_ns:           "Serviceaccount_ns",
			}

			err = dbq.CreateClusterCredentials(ctx, &clusterCredentials)
			Expect(err).ToNot(HaveOccurred())

			var gitopsEngineCluster db.GitopsEngineCluster
			var gitopsEngineInstance db.GitopsEngineInstance
			var clusterAccess db.ClusterAccess
			var managedEnvironment db.ManagedEnvironment

			// Create managed environment, and cluster access, so the non-unsafe get works below
			{
				managedEnvironment = db.ManagedEnvironment{
					Managedenvironment_id: "test-managed-env-914",
					Clustercredentials_id: clusterCredentials.Clustercredentials_cred_id,
					Name:                  "my env",
				}
				err = dbq.CreateManagedEnvironment(ctx, &managedEnvironment)
				Expect(err).ToNot(HaveOccurred())

				gitopsEngineCluster = db.GitopsEngineCluster{
					Gitopsenginecluster_id: "test-fake-cluster-914",
					Clustercredentials_id:  clusterCredentials.Clustercredentials_cred_id,
				}
				err = dbq.CreateGitopsEngineCluster(ctx, &gitopsEngineCluster)
				Expect(err).ToNot(HaveOccurred())

				gitopsEngineInstance = db.GitopsEngineInstance{
					Gitopsengineinstance_id: "test-fake-engine-instance-id",
					Namespace_name:          "test-fake-namespace",
					Namespace_uid:           "test-fake-namespace-914",
					EngineCluster_id:        gitopsEngineCluster.Gitopsenginecluster_id,
				}
				err = dbq.CreateGitopsEngineInstance(ctx, &gitopsEngineInstance)
				Expect(err).ToNot(HaveOccurred())

				clusterAccess = db.ClusterAccess{
					Clusteraccess_user_id:                   testClusterUser.Clusteruser_id,
					Clusteraccess_managed_environment_id:    managedEnvironment.Managedenvironment_id,
					Clusteraccess_gitops_engine_instance_id: gitopsEngineInstance.Gitopsengineinstance_id,
				}
				err = dbq.CreateClusterAccess(ctx, &clusterAccess)
				Expect(err).ToNot(HaveOccurred())

			}

			retrievedClusterCredentials := &db.ClusterCredentials{
				Clustercredentials_cred_id: clusterCredentials.Clustercredentials_cred_id,
			}
			err = dbq.GetClusterCredentialsById(ctx, retrievedClusterCredentials)
			Expect(err).ToNot(HaveOccurred())

			Expect(clusterCredentials.Host).Should(Equal(retrievedClusterCredentials.Host))
			Expect(clusterCredentials.Kube_config).Should(Equal(retrievedClusterCredentials.Kube_config))
			Expect(clusterCredentials.Kube_config_context).Should(Equal(retrievedClusterCredentials.Kube_config_context))

			retrievedClusterCredentials = &db.ClusterCredentials{
				Clustercredentials_cred_id: clusterCredentials.Clustercredentials_cred_id,
			}
			err = dbq.CheckedGetClusterCredentialsById(ctx, retrievedClusterCredentials, testClusterUser.Clusteruser_id)
			Expect(err).ToNot(HaveOccurred())
			Expect(retrievedClusterCredentials).ToNot(BeNil())

			Expect(clusterCredentials.Host).Should(Equal(retrievedClusterCredentials.Host))
			Expect(clusterCredentials.Kube_config).Should(Equal(retrievedClusterCredentials.Kube_config))
			Expect(clusterCredentials.Kube_config_context).Should(Equal(retrievedClusterCredentials.Kube_config_context))

			rowsAffected, err := dbq.DeleteClusterAccessById(ctx, clusterAccess.Clusteraccess_user_id, clusterAccess.Clusteraccess_managed_environment_id, clusterAccess.Clusteraccess_gitops_engine_instance_id)
			Expect(rowsAffected).Should(Equal(1))
			Expect(err).ToNot(HaveOccurred())

			rowsAffected, err = dbq.DeleteGitopsEngineInstanceById(ctx, gitopsEngineInstance.Gitopsengineinstance_id)
			Expect(rowsAffected).Should(Equal(1))
			Expect(err).ToNot(HaveOccurred())

			rowsAffected, err = dbq.DeleteGitopsEngineClusterById(ctx, gitopsEngineCluster.Gitopsenginecluster_id)
			Expect(rowsAffected).Should(Equal(1))
			Expect(err).ToNot(HaveOccurred())

			rowsAffected, err = dbq.DeleteManagedEnvironmentById(ctx, managedEnvironment.Managedenvironment_id)
			Expect(rowsAffected).Should(Equal(1))
			Expect(err).ToNot(HaveOccurred())

			rowsAffected, err = dbq.DeleteClusterCredentialsById(ctx, clusterCredentials.Clustercredentials_cred_id)
			Expect(rowsAffected).Should(Equal(1))
			Expect(err).ToNot(HaveOccurred())

			retrievedClusterCredentials = &db.ClusterCredentials{
				Clustercredentials_cred_id: clusterCredentials.Clustercredentials_cred_id,
			}
			err = dbq.GetClusterCredentialsById(ctx, retrievedClusterCredentials)
			Expect(err).To(HaveOccurred())
			Expect(db.IsResultNotFoundError(err)).To(BeTrue())
		})

	})
})
