// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.
//

package model

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestInstallationClone(t *testing.T) {
	installation := &Installation{
		ID:       "id",
		OwnerID:  "owner",
		Version:  "version",
		DNS:      "test.example.com",
		License:  "this_is_my_license",
		Affinity: InstallationAffinityIsolated,
		GroupID:  sToP("group_id"),
		State:    InstallationStateStable,
	}

	clone := installation.Clone()
	require.Equal(t, installation, clone)

	// Verify changing pointers in the clone doesn't affect the original.
	clone.GroupID = sToP("new_group_id")
	require.NotEqual(t, installation, clone)
}

func TestInstallationFromReader(t *testing.T) {
	t.Run("empty request", func(t *testing.T) {
		installation, err := InstallationFromReader(bytes.NewReader([]byte(
			``,
		)))
		require.NoError(t, err)
		require.Equal(t, &Installation{}, installation)
	})

	t.Run("invalid request", func(t *testing.T) {
		installation, err := InstallationFromReader(bytes.NewReader([]byte(
			`{test`,
		)))
		require.Error(t, err)
		require.Nil(t, installation)
	})

	t.Run("request", func(t *testing.T) {
		installation, err := InstallationFromReader(bytes.NewReader([]byte(`{
			"ID":"id",
			"OwnerID":"owner",
			"GroupID":"group_id",
			"Version":"version",
			"DNS":"dns",
			"License": "this_is_my_license",
			"MattermostEnv": {"key1": {"Value": "value1"}},
			"Affinity":"affinity",
			"State":"state",
			"CreateAt":10,
			"DeleteAt":20,
			"LockAcquiredAt":0
		}`)))
		require.NoError(t, err)
		require.Equal(t, &Installation{
			ID:             "id",
			OwnerID:        "owner",
			GroupID:        sToP("group_id"),
			Version:        "version",
			DNS:            "dns",
			License:        "this_is_my_license",
			MattermostEnv:  EnvVarMap{"key1": {Value: "value1"}},
			Affinity:       "affinity",
			State:          "state",
			CreateAt:       10,
			DeleteAt:       20,
			LockAcquiredBy: nil,
			LockAcquiredAt: int64(0),
		}, installation)
	})
}

func TestInstallationsFromReader(t *testing.T) {
	t.Run("empty request", func(t *testing.T) {
		installations, err := InstallationsFromReader(bytes.NewReader([]byte(
			``,
		)))
		require.NoError(t, err)
		require.Equal(t, []*Installation{}, installations)
	})

	t.Run("invalid request", func(t *testing.T) {
		installations, err := InstallationsFromReader(bytes.NewReader([]byte(
			`{test`,
		)))
		require.Error(t, err)
		require.Nil(t, installations)
	})

	t.Run("request", func(t *testing.T) {
		installation, err := InstallationsFromReader(bytes.NewReader([]byte(`[
			{
				"ID":"id1",
				"OwnerID":"owner1",
				"GroupID":"group_id1",
				"Version":"version1",
				"DNS":"dns1",
				"MattermostEnv": {"key1": {"Value": "value1"}},
				"Affinity":"affinity1",
				"State":"state1",
				"CreateAt":10,
				"DeleteAt":20,
				"LockAcquiredAt":0
			},
			{
				"ID":"id2",
				"OwnerID":"owner2",
				"GroupID":"group_id2",
				"Version":"version2",
				"DNS":"dns2",
				"License": "this_is_my_license",
				"MattermostEnv": {"key2": {"Value": "value2"}},
				"Affinity":"affinity2",
				"State":"state2",
				"CreateAt":30,
				"DeleteAt":40,
				"LockAcquiredBy": "tester",
				"LockAcquiredAt":50
			}
		]`)))
		require.NoError(t, err)
		require.Equal(t, []*Installation{
			{
				ID:             "id1",
				OwnerID:        "owner1",
				GroupID:        sToP("group_id1"),
				Version:        "version1",
				DNS:            "dns1",
				MattermostEnv:  EnvVarMap{"key1": {Value: "value1"}},
				Affinity:       "affinity1",
				State:          "state1",
				CreateAt:       10,
				DeleteAt:       20,
				LockAcquiredBy: nil,
				LockAcquiredAt: 0,
			}, {
				ID:             "id2",
				OwnerID:        "owner2",
				GroupID:        sToP("group_id2"),
				Version:        "version2",
				DNS:            "dns2",
				License:        "this_is_my_license",
				MattermostEnv:  EnvVarMap{"key2": {Value: "value2"}},
				Affinity:       "affinity2",
				State:          "state2",
				CreateAt:       30,
				DeleteAt:       40,
				LockAcquiredBy: sToP("tester"),
				LockAcquiredAt: 50,
			},
		}, installation)
	})
}

func TestMergeWithGroup(t *testing.T) {
	checkMergeValues := func(t *testing.T, installation *Installation, group *Group) {
		t.Helper()

		assert.Equal(t, installation.GroupID != nil, installation.IsInGroup())

		assert.Equal(t, installation.Version, group.Version)
		assert.Equal(t, installation.Image, group.Image)

		// TODO: check normal installation env settings that aren't overridden.
		for key := range group.MattermostEnv {
			assert.Equal(t, installation.MattermostEnv[key].Value, group.MattermostEnv[key].Value)
		}
	}

	t.Run("without overrides", func(t *testing.T) {
		installation := &Installation{
			ID:       NewID(),
			OwnerID:  "owner",
			Version:  "iversion",
			Image:    "iImage",
			DNS:      "test.example.com",
			License:  "this_is_my_license",
			Affinity: InstallationAffinityIsolated,
			GroupID:  sToP("group_id"),
			State:    InstallationStateStable,
		}

		group := &Group{
			ID:      NewID(),
			Version: "gversion",
			Image:   "gImage",
			MattermostEnv: EnvVarMap{
				"key1": EnvVar{
					Value: "value1",
				},
			},
		}

		installation.MergeWithGroup(group, false)
		checkMergeValues(t, installation, group)
	})

	t.Run("with overrides, no env overrides found", func(t *testing.T) {
		installation := &Installation{
			ID:       NewID(),
			OwnerID:  "owner",
			Version:  "iversion",
			Image:    "iImage",
			DNS:      "test.example.com",
			License:  "this_is_my_license",
			Affinity: InstallationAffinityIsolated,
			GroupID:  sToP("group_id"),
			State:    InstallationStateStable,
		}

		group := &Group{
			ID:      NewID(),
			Version: "gversion",
			Image:   "gImage",
			MattermostEnv: EnvVarMap{
				"key1": EnvVar{
					Value: "value1",
				},
			},
		}

		installation.MergeWithGroup(group, true)
		checkMergeValues(t, installation, group)
		assert.NotEmpty(t, installation.GroupOverrides)
	})

	t.Run("with overrides, env overrides found", func(t *testing.T) {
		installation := &Installation{
			ID:       NewID(),
			OwnerID:  "owner",
			Version:  "iversion",
			Image:    "iImage",
			DNS:      "test.example.com",
			License:  "this_is_my_license",
			Affinity: InstallationAffinityIsolated,
			GroupID:  sToP("group_id"),
			State:    InstallationStateStable,
			MattermostEnv: EnvVarMap{
				"key2": EnvVar{
					Value: "ivalue1",
				},
			},
		}

		group := &Group{
			ID:      NewID(),
			Version: "gversion",
			Image:   "gImage",
			MattermostEnv: EnvVarMap{
				"key1": EnvVar{
					Value: "value1",
				},
				"key2": EnvVar{
					Value: "value2",
				},
			},
		}

		installation.MergeWithGroup(group, true)
		checkMergeValues(t, installation, group)
		assert.NotEmpty(t, installation.GroupOverrides)
	})

	t.Run("without overrides, group sequence matches", func(t *testing.T) {
		installation := &Installation{
			ID:            NewID(),
			OwnerID:       "owner",
			Version:       "iversion",
			Image:         "iImage",
			DNS:           "test.example.com",
			License:       "this_is_my_license",
			Affinity:      InstallationAffinityIsolated,
			GroupID:       sToP("group_id"),
			GroupSequence: iToP(2),
			State:         InstallationStateStable,
		}

		group := &Group{
			ID:       NewID(),
			Sequence: 2,
			Version:  "gversion",
			Image:    "gImage",
			MattermostEnv: EnvVarMap{
				"key1": EnvVar{
					Value: "value1",
				},
			},
		}

		installation.MergeWithGroup(group, false)
		checkMergeValues(t, installation, group)
		assert.True(t, installation.InstallationSequenceMatchesMergedGroupSequence())
	})

	t.Run("without overrides, group sequence doesn't match", func(t *testing.T) {
		installation := &Installation{
			ID:            NewID(),
			OwnerID:       "owner",
			Version:       "iversion",
			Image:         "iImage",
			DNS:           "test.example.com",
			License:       "this_is_my_license",
			Affinity:      InstallationAffinityIsolated,
			GroupID:       sToP("group_id"),
			GroupSequence: iToP(1),
			State:         InstallationStateStable,
		}

		group := &Group{
			ID:       NewID(),
			Sequence: 2,
			Version:  "gversion",
			Image:    "gImage",
			MattermostEnv: EnvVarMap{
				"key1": EnvVar{
					Value: "value1",
				},
			},
		}

		installation.MergeWithGroup(group, false)
		checkMergeValues(t, installation, group)
		assert.False(t, installation.InstallationSequenceMatchesMergedGroupSequence())
	})
}

func TestInstallation_GetEnvVars(t *testing.T) {
	for _, testCase := range []struct {
		description  string
		installation Installation
		expectedEnv  EnvVarMap
	}{
		{
			description:  "no envs",
			installation: Installation{},
			expectedEnv:  EnvVarMap{},
		},
		{
			description: "use regular envs",
			installation: Installation{MattermostEnv: EnvVarMap{
				"MM_TEST":  EnvVar{Value: "test"},
				"MM_TEST2": EnvVar{ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "test"}}}},
			},
			expectedEnv: EnvVarMap{
				"MM_TEST":  EnvVar{Value: "test"},
				"MM_TEST2": EnvVar{ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "test"}}},
			},
		},
		{
			description: "prioritize priority envs",
			installation: Installation{
				MattermostEnv: EnvVarMap{
					"MM_TEST":  EnvVar{Value: "test"},
					"MM_TEST2": EnvVar{ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Key: "test"}}},
				},
				PriorityEnv: EnvVarMap{
					"MM_TEST2": EnvVar{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "secret-test"}}},
					"MM_TEST3": EnvVar{Value: "test3"},
				},
			},
			expectedEnv: EnvVarMap{
				"MM_TEST":  EnvVar{Value: "test"},
				"MM_TEST2": EnvVar{ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "secret-test"}}},
				"MM_TEST3": EnvVar{Value: "test3"},
			},
		},
	} {
		t.Run(testCase.description, func(t *testing.T) {
			envs := testCase.installation.GetEnvVars()
			assert.Equal(t, testCase.expectedEnv, envs)
		})
	}
}
