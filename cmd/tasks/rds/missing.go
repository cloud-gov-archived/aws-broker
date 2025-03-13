package rds

import (
	"github.com/18F/aws-broker/catalog"
	"github.com/18F/aws-broker/services/rds"
	"github.com/aws/aws-sdk-go/service/rds/rdsiface"
	"github.com/jinzhu/gorm"
)

func ReconcileMissingResourcesForAllRDSDatabases(catalog *catalog.Catalog, db *gorm.DB, rdsClient rdsiface.RDSAPI) error {
	rows, err := db.Model(&rds.RDSInstance{}).Rows()
	if err != nil {
		return err
	}

	var errs error

	for rows.Next() {
		var rdsInstance rds.RDSInstance
		db.ScanRows(rows, &rdsInstance)

		// stub out logic to check if RDS database exists
		// stub out logic to check if CF instance exists

		// if CF + RDS instance are misssing, then delete record from broker
		// if CF instance is missing and RDS database exists, then delete RDS database?
		// if RDS instance is missing and CF instance exists, then ?
	}

	return errs
}
