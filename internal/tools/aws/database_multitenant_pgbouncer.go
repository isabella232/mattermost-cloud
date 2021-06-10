// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
//

package aws

import (
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"

	"github.com/mattermost/mattermost-cloud/model"
	// Database drivers
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// RDSMultitenantDatabase is a database backed by RDS that supports multi-tenancy.
type RDSMultitenantPGBouncerDatabase struct {
	databaseType   string
	installationID string
	instanceID     string
	db             SQLDatabaseManager
	client         *Client
}

// NewRDSMultitenantDatabase returns a new instance of RDSMultitenantDatabase that implements database interface.
func NewRDSMultitenantPGBouncerDatabase(databaseType, instanceID, installationID string, client *Client) *RDSMultitenantDatabase {
	return &RDSMultitenantDatabase{
		databaseType:   databaseType,
		instanceID:     instanceID,
		installationID: installationID,
		client:         client,
	}
}

// IsValid returns if the given RDSMultitenantDatabase configuration is valid.
func (d *RDSMultitenantPGBouncerDatabase) IsValid() error {
	if len(d.installationID) == 0 {
		return errors.New("installation ID is not set")
	}

	switch d.databaseType {
	case model.DatabaseEngineTypeMySQL,
		model.DatabaseEngineTypePostgres:
	default:
		return errors.Errorf("invalid database type %s", d.databaseType)
	}

	return nil
}

// DatabaseTypeTagValue returns the tag value used for filtering RDS cluster
// resources based on database type.
func (d *RDSMultitenantPGBouncerDatabase) DatabaseTypeTagValue() string {
	if d.databaseType == model.DatabaseEngineTypeMySQL {
		return DatabaseTypeMySQLAurora
	}

	return DatabaseTypePostgresSQLAurora
}

// MaxSupportedDatabases returns the maximum number of databases supported on
// one RDS cluster for this database type.
func (d *RDSMultitenantPGBouncerDatabase) MaxSupportedDatabases() int {
	if d.databaseType == model.DatabaseEngineTypeMySQL {
		return DefaultRDSMultitenantDatabaseMySQLCountLimit
	}

	return DefaultRDSMultitenantDatabasePostgresCountLimit
}

// Provision claims a multitenant RDS cluster and creates a database schema for
// the installation.
func (d *RDSMultitenantPGBouncerDatabase) Provision(store model.InstallationDatabaseStoreInterface, logger log.FieldLogger) error {
	return errors.New("NOT DONE")
}

// Snapshot creates a snapshot of single RDS multitenant database.
func (d *RDSMultitenantPGBouncerDatabase) Snapshot(store model.InstallationDatabaseStoreInterface, logger log.FieldLogger) error {
	return errors.New("not implemented")
}

// GenerateDatabaseSecret creates the k8s database spec and secret for
// accessing a single database inside a RDS multitenant cluster.
func (d *RDSMultitenantPGBouncerDatabase) GenerateDatabaseSecret(store model.InstallationDatabaseStoreInterface, logger log.FieldLogger) (*corev1.Secret, error) {
	return nil, errors.New("NOT DONE")
}

// Teardown removes all AWS resources related to a RDS multitenant database.
func (d *RDSMultitenantPGBouncerDatabase) Teardown(store model.InstallationDatabaseStoreInterface, keepData bool, logger log.FieldLogger) error {
	return errors.New("NOT DONE")
}

const baseIni = `
[pgbouncer]
listen_addr = *
listen_port = 5432
auth_file = /etc/userlist/userlist.txt
admin_users = admin
ignore_startup_parameters = extra_float_digits
pool_mode = transaction
min_pool_size = 20
default_pool_size = 20
reserve_pool_size = 5
max_client_conn = 10000
max_db_connections = 20
[databases]
`

func generatePGBouncerIni() string {
	ini := baseIni

	databases := []string{"blah"}
	for _, database := range databases {

	}
	return ini
}
