package manage

import (
	"context"
	"encoding/json"
	"os"
	"sort"

	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
)

// BucketOpener builds the object storage client the two bucket commands work through. It is a function rather than a store so a command that is never run does not have to connect.
type BucketOpener func() (*storage.Store, error)

// Buckets is how the two bucket commands reach object storage. A binary that does not set it answers the way a missing bucket name does.
var Buckets BucketOpener

func bucketStore() (*storage.Store, error) {
	if Buckets == nil {
		return nil, commandError("Please set the AWS_S3_BUCKET_NAME environment variable.")
	}
	store, err := Buckets()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, commandError("Please set the AWS_S3_BUCKET_NAME environment variable.")
	}
	return store, nil
}

// createBucket is manage.py create_bucket: make the default bucket when it is not already there.
//
// Every failure is reported and swallowed, so the command answers zero whatever happened — which is what lets it sit in a startup script ahead of the server.
func createBucket(ctx context.Context, env Environment, _ []string) error {
	store, err := bucketStore()
	if err != nil {
		write(env, "%s", err)
		return nil
	}
	write(env, "Checking bucket...")
	created, err := store.EnsureBucket(ctx)
	if err != nil {
		write(env, "Failed to check bucket: %s", err)
		return nil
	}
	if created {
		write(env, "Bucket '%s' does not exist. Creating bucket...", store.Bucket())
		write(env, "Bucket '%s' created successfully.", store.Bucket())
		return nil
	}
	write(env, "Bucket '%s' exists.", store.Bucket())
	return nil
}

// updateBucket is manage.py update_bucket: keep the objects already in the bucket readable after the bucket itself stops being public.
//
// It checks the four permissions it needs by using them, and only writes the policy when it has all four. Without them it writes the policy it would have applied into permissions.json for somebody to apply by hand.
func updateBucket(ctx context.Context, env Environment, _ []string) error {
	store, err := bucketStore()
	if err != nil {
		write(env, "%s", err)
		return nil
	}
	write(env, "Checking bucket...")
	// This one only asks. A bucket that is not there is reported rather than made, which is what keeps create_bucket and update_bucket different commands.
	exists, err := store.BucketExists(ctx)
	if err != nil {
		write(env, "Error: %s", err)
	} else if !exists {
		write(env, "Bucket '%s' does not exist.", store.Bucket())
		return nil
	}
	write(env, "Bucket '%s' exists.", store.Bucket())

	permissions := checkBucketPermissions(ctx, env, store)
	if allPermitted(permissions) {
		write(env, "Access key has the required permissions.")
		policy, err := publicObjectPolicy(ctx, store)
		if err != nil {
			write(env, "Error: %s", err)
			return nil
		}
		if err := store.SetBucketPolicy(ctx, policy); err != nil {
			write(env, "Error: %s", err)
			return nil
		}
		write(env, "Bucket is private, but existing objects remain public.")
		return nil
	}

	write(env, "Generating permissions.json for manual bucket policy update.")
	policy, err := publicObjectPolicy(ctx, store)
	if err != nil {
		write(env, "Error writing permissions.json: %s", err)
		return nil
	}
	if err := os.WriteFile("permissions.json", []byte(policy), 0o644); err != nil {
		write(env, "Error writing permissions.json: %s", err)
		return nil
	}
	write(env, "Permissions have been written to permissions.json.")
	return nil
}

// bucketPermissions are the four the command tests by using them.
type bucketPermissions struct {
	GetObject, ListBucket, PutBucketPolicy, PutObject bool
}

func allPermitted(permissions bucketPermissions) bool {
	return permissions.GetObject && permissions.ListBucket && permissions.PutBucketPolicy && permissions.PutObject
}

// checkBucketPermissions is check_s3_permissions: each one is proven by doing it, and the object written to prove the third is deleted afterwards.
//
// GetObject is only tested when the bucket already holds something, so an empty bucket never proves it and the command falls through to writing permissions.json. That is upstream's behaviour and is kept.
func checkBucketPermissions(ctx context.Context, env Environment, store *storage.Store) bucketPermissions {
	permissions := bucketPermissions{}

	keys, err := store.ListObjectKeys(ctx, 1000)
	if err != nil {
		write(env, "Error in ListBucket: %s", err)
	} else {
		permissions.ListBucket = true
	}

	if len(keys) > 0 {
		if err := store.GetObjectBytes(ctx, keys[0]); err != nil {
			write(env, "Error in GetObject: %s", err)
		} else {
			permissions.GetObject = true
		}
	}

	const probe = "test_permission_check.txt"
	if err := store.PutObject(ctx, probe, "text/plain", []byte("Test"), false); err != nil {
		write(env, "Error in PutObject: %s", err)
	} else {
		permissions.PutObject = true
	}
	if err := store.RemoveObject(ctx, probe); err != nil {
		write(env, "Couldn't delete test object")
	}

	policy, err := wholeBucketPolicy(store.Bucket())
	if err == nil {
		if err := store.SetBucketPolicy(ctx, policy); err != nil {
			write(env, "Error in PutBucketPolicy: %s", err)
		} else {
			permissions.PutBucketPolicy = true
		}
	}
	return permissions
}

// wholeBucketPolicy is the policy the permission check writes: everything in the bucket readable. It is written to prove the key may write a policy at all, and the one that really matters is written over it afterwards.
func wholeBucketPolicy(bucket string) (string, error) {
	return renderPolicy([]string{"arn:aws:s3:::" + bucket + "/*"})
}

// publicObjectPolicy is generate_bucket_policy: every object the bucket holds right now, named one by one.
//
// Naming them rather than using a wildcard is the whole point — what is already there stays readable and what is written later does not.
func publicObjectPolicy(ctx context.Context, store *storage.Store) (string, error) {
	keys, err := store.ListObjectKeys(ctx, 0)
	if err != nil {
		return "", err
	}
	sort.Strings(keys)
	resources := make([]string, 0, len(keys))
	for _, key := range keys {
		resources = append(resources, "arn:aws:s3:::"+store.Bucket()+"/"+key)
	}
	return renderPolicy(resources)
}

func renderPolicy(resources []string) (string, error) {
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []any{map[string]any{
			"Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": resources,
		}},
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
