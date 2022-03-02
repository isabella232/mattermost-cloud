// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
//

package cloudflare

import (
	"context"
	"fmt"
	"time"

	cf "github.com/cloudflare/cloudflare-go"
	"github.com/pkg/errors"

	"github.com/sirupsen/logrus"
)

const defaultTimeout = 30 * time.Second

type Client struct {
	cfClient *cf.API
}

// NewClientWithToken creates a new client that can be used to run the other functions.
func NewClientWithToken(token string) (*Client, error) {
	client, err := cf.NewWithAPIToken(token)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to initialize cloudflare client using API token")
	}
	return &Client{
		cfClient: client,
	}, nil
}

func (c *Client) getZoneId(zoneName string, logger logrus.FieldLogger) (zoneID string, err error) {
	zoneID, err = c.cfClient.ZoneIDByName(zoneName)
	if err != nil {
		return "", err
	}

	return zoneID, err
}

func (c *Client) getRecordId(zoneID, customerDnsName string, logger logrus.FieldLogger) (recordID string, err error) {

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	dnsRecords, err := c.cfClient.DNSRecords(ctx, zoneID, cf.DNSRecord{Name: customerDnsName})
	if err != nil {
		logger.WithError(err).Error("failed to get DNS Record ID from Cloudflare")
		return "", err
	}
	if len(dnsRecords) == 0 {
		logger.Info("Unable to find any DNS records in Cloudflare; skipping...")
		return "", nil
	}

	return dnsRecords[0].ID, nil

}

func (c *Client) CreateDNSRecord(customerDnsName string, zoneNameList []string, dnsEndpoint string, logger logrus.FieldLogger) error {

	// Fetch the zone ID
	for _, zoneName := range zoneNameList {
		zoneID, err := c.getZoneId(zoneName, logger)
		if err != nil {
			logger.Infof("Unable to find the zone name %s in Cloudflare; skipping...", zoneName)
			break
		}

		proxied := true

		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		recordResp, err := c.cfClient.CreateDNSRecord(ctx, zoneID, cf.DNSRecord{
			Name:    customerDnsName,
			Type:    "CNAME",
			Content: dnsEndpoint,
			TTL:     1,
			Proxied: &proxied,
		})
		if err != nil {
			logger.WithError(err).Error("failed to create DNS Record at Cloudflare")
			return err
		}
		fmt.Println(recordResp)

		logger.WithFields(logrus.Fields{
			"cloudflare-dns-value":    customerDnsName,
			"cloudflare-dns-endpoint": dnsEndpoint,
			"cloudflare-zone-id":      zoneID,
		}).Debugf("Cloudflare create DNS record response: %s", recordResp)
	}
	return nil
}

// DeleteDNSRecord gets DNS name and zone name which uses to delete that DNS record from Cloudflare
func (c *Client) DeleteDNSRecord(customerDnsName string, zoneNameList []string, logger logrus.FieldLogger) error {

	for _, zoneName := range zoneNameList {
		zoneID, err := c.getZoneId(zoneName, logger)
		if err != nil {
			logger.Infof("Unable to find the zone name %s in Cloudflare; skipping...", zoneName)
			break
		}

		recordID, err := c.getRecordId(zoneID, customerDnsName, logger)
		if err != nil {
			logger.WithError(err).Errorf("Failed to get record ID from Cloudflare for DNS: %s", customerDnsName)
			return err
		}

		// Unable to find any record, skipping deletion
		if err == nil && recordID == "" {
			logger.Info("Unable to find any DNS records in Cloudflare; skipping...")
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()

		err = c.cfClient.DeleteDNSRecord(ctx, zoneID, recordID)
		if err != nil {
			logger.WithError(err).Error("Failed to delete DNS Record at Cloudflare")
			return err
		}
	}
	return nil
}

//func (c *Client) UpdateDNSRecord(customerDnsName, zoneName, newEndpoint string, logger logrus.FieldLogger) error {
//	zoneID, err := c.getZoneId(zoneName)
//	if err != nil {
//		logger.WithError(err).Error("failed to get zone ID from Cloudflare")
//		return err
//	}
//
//	recordID, err := c.getRecordId(zoneID, customerDnsName)
//	if err != nil {
//		logger.WithError(err).Error("failed to get record ID from Cloudflare")
//		return err
//	}
//
//	proxied := true
//
//	// Most API calls require a Context
//	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
//	defer cancel()
//
//	err = c.cfClient.UpdateDNSRecord(ctx, zoneID, recordID, cf.DNSRecord{
//		Name:    customerDnsName,
//		Type:    "CNAME",
//		Content: newEndpoint,
//		TTL:     1,
//		Proxied: &proxied,
//	})
//	if err != nil {
//		logger.WithError(err).Error("failed to update record ID from Cloudflare")
//		return err
//	}
//
//	return nil
//
//}
