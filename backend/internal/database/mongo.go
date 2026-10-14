package database

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Mongo bundles the client (connection pool) with the app's database handle.
type Mongo struct {
	Client *mongo.Client
	DB     *mongo.Database
}

// NewMongo connects to MongoDB and checks that the server is reachable.
// The caller must call Close() on shutdown.
func NewMongo(ctx context.Context, uri, dbName string) (*Mongo, error) {
	opts := options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(5 * time.Second)

	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("mongo: connect: %w", err)
	}

	m := &Mongo{Client: client, DB: client.Database(dbName)}

	// Like pgxpool, the driver connects lazily, so ping once to fail fast.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.Ping(pingCtx); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo: ping: %w", err)
	}

	return m, nil
}

// Ping checks the connection to the primary node.
// Its signature matches handler.Pinger, which the driver's own Ping(ctx, readpref) does not.
func (m *Mongo) Ping(ctx context.Context) error {
	return m.Client.Ping(ctx, readpref.Primary())
}

// Close disconnects all pooled connections.
func (m *Mongo) Close(ctx context.Context) error {
	return m.Client.Disconnect(ctx)
}
