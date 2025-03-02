// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
//

package supervisor

import (
	"github.com/mattermost/mattermost-cloud/internal/metrics"
	"github.com/mattermost/mattermost-cloud/internal/provisioner"
	"github.com/pkg/errors"

	log "github.com/sirupsen/logrus"

	"github.com/mattermost/mattermost-cloud/internal/tools/aws"
	"github.com/mattermost/mattermost-cloud/model"
)

// clusterInstallationStore abstracts the database operations required to query installations.
type clusterInstallationStore interface {
	GetCluster(clusterID string) (*model.Cluster, error)

	GetInstallation(installationID string, includeGroupConfig, includeGroupConfigOverrides bool) (*model.Installation, error)

	GetClusterInstallation(clusterInstallationID string) (*model.ClusterInstallation, error)
	GetUnlockedClusterInstallationsPendingWork() ([]*model.ClusterInstallation, error)
	LockClusterInstallations(clusterInstallationID []string, lockerID string) (bool, error)
	UnlockClusterInstallations(clusterInstallationID []string, lockerID string, force bool) (bool, error)
	UpdateClusterInstallation(clusterInstallation *model.ClusterInstallation) error
	DeleteClusterInstallation(clusterInstallationID string) error

	GetInstallationBackups(filter *model.InstallationBackupFilter) ([]*model.InstallationBackup, error)

	GetMultitenantDatabases(filter *model.MultitenantDatabaseFilter) ([]*model.MultitenantDatabase, error)
	GetLogicalDatabases(filter *model.LogicalDatabaseFilter) ([]*model.LogicalDatabase, error)

	GetStateChangeEvents(filter *model.StateChangeEventFilter) ([]*model.StateChangeEventData, error)

	GetWebhooks(filter *model.WebhookFilter) ([]*model.Webhook, error)
}

// clusterInstallationProvisioner abstracts the provisioning operations required by the cluster installation supervisor.
type clusterInstallationProvisioner interface {
	ClusterInstallationProvisioner(version string) provisioner.ClusterInstallationProvisioner
}

// ClusterInstallationSupervisor finds cluster installations pending work and effects the required changes.
//
// The degree of parallelism is controlled by a weighted semaphore, intended to be shared with
// other clients needing to coordinate background jobs.
type ClusterInstallationSupervisor struct {
	store          clusterInstallationStore
	provisioner    clusterInstallationProvisioner
	aws            aws.AWS
	eventsProducer eventProducer
	instanceID     string
	logger         log.FieldLogger
	metrics        *metrics.CloudMetrics
}

// NewClusterInstallationSupervisor creates a new ClusterInstallationSupervisor.
func NewClusterInstallationSupervisor(store clusterInstallationStore, clusterInstallationProvisioner clusterInstallationProvisioner, aws aws.AWS, eventsProducer eventProducer, instanceID string, logger log.FieldLogger, metrics *metrics.CloudMetrics) *ClusterInstallationSupervisor {
	return &ClusterInstallationSupervisor{
		store:          store,
		provisioner:    clusterInstallationProvisioner,
		aws:            aws,
		eventsProducer: eventsProducer,
		instanceID:     instanceID,
		logger:         logger,
		metrics:        metrics,
	}
}

// Shutdown performs graceful shutdown tasks for the cluster installation supervisor.
func (s *ClusterInstallationSupervisor) Shutdown() {
	s.logger.Debug("Shutting down cluster installation supervisor")
}

// Do looks for work to be done on any pending cluster installations and attempts to schedule the required work.
func (s *ClusterInstallationSupervisor) Do() error {
	clusterInstallations, err := s.store.GetUnlockedClusterInstallationsPendingWork()
	if err != nil {
		s.logger.WithError(err).Warn("Failed to query for cluster installations pending work")
		return nil
	}

	for _, clusterInstallation := range clusterInstallations {
		s.Supervise(clusterInstallation)
	}

	return nil
}

// Supervise schedules the required work on the given cluster installation.
func (s *ClusterInstallationSupervisor) Supervise(clusterInstallation *model.ClusterInstallation) {
	logger := s.logger.WithFields(log.Fields{
		"clusterInstallation": clusterInstallation.ID,
		"installation":        clusterInstallation.InstallationID,
	})

	lock := newClusterInstallationLock(clusterInstallation.ID, s.instanceID, s.store, logger)
	if !lock.TryLock() {
		return
	}
	defer lock.Unlock()

	// Before working on the cluster installation, it is crucial that we ensure
	// that it was not updated to a new state by another provisioning server.
	originalState := clusterInstallation.State
	clusterInstallation, err := s.store.GetClusterInstallation(clusterInstallation.ID)
	if err != nil {
		logger.WithError(err).Errorf("Failed to get refreshed cluster installation")
		return
	}
	if clusterInstallation.State != originalState {
		logger.WithField("oldClusterInstallationState", originalState).
			WithField("newClusterInstallationState", clusterInstallation.State).
			Warn("Another provisioner has worked on this cluster installation; skipping...")
		return
	}

	logger.Debugf("Supervising cluster installation in state %s", clusterInstallation.State)

	newState := s.transitionClusterInstallation(clusterInstallation, logger)

	clusterInstallation, err = s.store.GetClusterInstallation(clusterInstallation.ID)
	if err != nil {
		logger.WithError(err).Warnf("failed to get cluster installation and thus persist state %s", newState)
		return
	}

	if clusterInstallation.State == newState {
		return
	}

	oldState := clusterInstallation.State
	clusterInstallation.State = newState
	err = s.store.UpdateClusterInstallation(clusterInstallation)
	if err != nil {
		logger.WithError(err).Errorf("failed to set cluster installation state to %s", newState)
		return
	}

	err = s.processClusterInstallationMetrics(clusterInstallation, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to process cluster installation metrics")
	}

	err = s.eventsProducer.ProduceClusterInstallationStateChangeEvent(clusterInstallation, oldState)
	if err != nil {
		logger.WithError(err).Error("Failed to create cluster installation state change event")
	}

	logger.Debugf("Transitioned cluster installation from %s to %s", oldState, newState)
}

func failedClusterInstallationState(state string) string {
	switch state {
	case model.ClusterInstallationStateCreationRequested:
		return model.ClusterInstallationStateCreationFailed
	case model.ClusterInstallationStateDeletionRequested:
		return model.ClusterInstallationStateDeletionFailed

	default:
		return state
	}
}

// transitionClusterInstallation works with the given cluster installation to transition it to a final state.
func (s *ClusterInstallationSupervisor) transitionClusterInstallation(clusterInstallation *model.ClusterInstallation, logger log.FieldLogger) string {
	cluster, err := s.store.GetCluster(clusterInstallation.ClusterID)
	if err != nil {
		logger.WithError(err).Warnf("Failed to query cluster %s", clusterInstallation.ClusterID)
		return clusterInstallation.State
	}
	if cluster == nil {
		logger.Errorf("Failed to find cluster %s", clusterInstallation.ClusterID)
		return failedClusterInstallationState(clusterInstallation.State)
	}

	installation, err := s.store.GetInstallation(clusterInstallation.InstallationID, true, false)
	if err != nil {
		logger.WithError(err).Warnf("Failed to query installation %s", clusterInstallation.InstallationID)
		return clusterInstallation.State
	}
	if installation == nil {
		logger.Errorf("Failed to find installation %s", clusterInstallation.InstallationID)
		return failedClusterInstallationState(clusterInstallation.State)
	}

	switch clusterInstallation.State {
	case model.ClusterInstallationStateCreationRequested:
		return s.createClusterInstallation(clusterInstallation, logger, installation, cluster)
	case model.ClusterInstallationStateDeletionRequested:
		return s.deleteClusterInstallation(clusterInstallation, logger, installation, cluster)
	case model.ClusterInstallationStateReconciling:
		return s.checkReconcilingClusterInstallation(clusterInstallation, installation, cluster, logger)
	default:
		logger.Warnf("Found cluster installation pending work in unexpected state %s", clusterInstallation.State)
		return clusterInstallation.State
	}
}

func (s *ClusterInstallationSupervisor) createClusterInstallation(clusterInstallation *model.ClusterInstallation, logger log.FieldLogger, installation *model.Installation, cluster *model.Cluster) string {
	err := s.provisioner.ClusterInstallationProvisioner(installation.CRVersion).
		PrepareClusterUtilities(cluster, installation, s.store, s.aws)
	if err != nil {
		logger.WithError(err).Error("Failed to provision cluster installation")
		return model.ClusterInstallationStateCreationRequested
	}

	err = s.provisioner.ClusterInstallationProvisioner(installation.CRVersion).
		CreateClusterInstallation(cluster, installation, clusterInstallation)
	if err != nil {
		logger.WithError(err).Error("Failed to provision cluster installation")
		return model.ClusterInstallationStateCreationRequested
	}

	err = s.store.UpdateClusterInstallation(clusterInstallation)
	if err != nil {
		logger.WithError(err).Error("Failed to record updated cluster installation after provisioning")
		return model.ClusterInstallationStateCreationFailed
	}

	logger.Info("Finished creating cluster installation")
	return model.ClusterInstallationStateReconciling
}

func (s *ClusterInstallationSupervisor) deleteClusterInstallation(clusterInstallation *model.ClusterInstallation, logger log.FieldLogger, installation *model.Installation, cluster *model.Cluster) string {
	backups, err := s.store.GetInstallationBackups(&model.InstallationBackupFilter{
		ClusterInstallationID: clusterInstallation.ID,
		States:                model.AllInstallationBackupsStatesRunning,
		Paging:                model.AllPagesNotDeleted(),
	})
	if err != nil {
		logger.WithError(err).Error("Failed to get installation backups running in cluster installation namespace")
		return clusterInstallation.State
	}
	if len(backups) > 0 {
		logger.Warn("Cannot delete cluster installation while backups are running in its namespace")
		return clusterInstallation.State
	}

	err = s.provisioner.ClusterInstallationProvisioner(installation.CRVersion).
		DeleteClusterInstallation(cluster, installation, clusterInstallation)
	if err != nil {
		logger.WithError(err).Error("Failed to delete cluster installation")
		return model.ClusterInstallationStateDeletionFailed
	}

	err = s.store.DeleteClusterInstallation(clusterInstallation.ID)
	if err != nil {
		logger.WithError(err).Error("Failed to record deleted cluster installation after deletion")
		return model.ClusterStateDeletionFailed
	}

	logger.Info("Finished deleting cluster installation")
	return model.ClusterInstallationStateDeleted
}

func (s *ClusterInstallationSupervisor) checkReconcilingClusterInstallation(clusterInstallation *model.ClusterInstallation, installation *model.Installation, cluster *model.Cluster, logger log.FieldLogger) string {
	isReady, err := s.provisioner.ClusterInstallationProvisioner(installation.CRVersion).
		IsResourceReady(cluster, clusterInstallation)
	if err != nil {
		logger.WithError(err).Error("Failed to get cluster installation resource")
		return model.ClusterInstallationStateReconciling
	}

	if !isReady {
		logger.Info("Cluster installation is still reconciling")
		return model.ClusterInstallationStateReconciling
	}

	err = s.provisioner.ClusterInstallationProvisioner(installation.CRVersion).
		DeleteOldClusterInstallationLicenseSecrets(cluster, installation, clusterInstallation)
	if err != nil {
		logger.WithError(err).Error("Failed to ensure old license secrets were deleted")
		return model.ClusterInstallationStateReconciling
	}

	logger.Info("Cluster installation finished reconciling")
	return model.ClusterInstallationStateStable
}

func (s *ClusterInstallationSupervisor) processClusterInstallationMetrics(clusterInstallation *model.ClusterInstallation, logger log.FieldLogger) error {
	if clusterInstallation.State != model.ClusterInstallationStateStable &&
		clusterInstallation.State != model.ClusterInstallationStateDeleted {
		return nil
	}

	// Get the latest event of a 'requested' type to emit the correct metrics.
	events, err := s.store.GetStateChangeEvents(&model.StateChangeEventFilter{
		ResourceID:   clusterInstallation.ID,
		ResourceType: model.TypeClusterInstallation,
		NewStates:    []string{model.ClusterInstallationStateReconciling, model.ClusterInstallationStateDeletionRequested},
		Paging:       model.Paging{Page: 0, PerPage: 1, IncludeDeleted: false},
	})
	if err != nil {
		return errors.Wrap(err, "failed to get state change events")
	}
	if len(events) != 1 {
		return errors.Errorf("expected 1 state change event, but got %d", len(events))
	}

	event := events[0]
	elapsedSeconds := model.ElapsedTimeInSeconds(event.Event.Timestamp)

	switch event.StateChange.NewState {
	case model.ClusterInstallationStateReconciling:
		s.metrics.ClusterInstallationReconcilingDurationHist.WithLabelValues(clusterInstallation.ClusterID).Observe(elapsedSeconds)
		logger.Debugf("Cluster installation was reconciled in %d seconds", int(elapsedSeconds))
	case model.ClusterInstallationStateDeletionRequested:
		s.metrics.ClusterInstallationDeletionDurationHist.WithLabelValues(clusterInstallation.ClusterID).Observe(elapsedSeconds)
		logger.Debugf("Cluster installation was deleted in %d seconds", int(elapsedSeconds))
	default:
		return errors.Errorf("failed to handle event %s with new state %s", event.Event.ID, event.StateChange.NewState)
	}

	return nil
}
