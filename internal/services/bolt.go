package services

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	bolt "go.etcd.io/bbolt"
)

// BoltDB implements the Store interface using a BoltDB backend for persistent storage of chats and
// messages. It provides atomic operations for managing chat histories and their associated messages
// through a key-value storage model.
type BoltDB struct {
	db *bolt.DB
}

// NewBoltDB creates a new BoltDB instance with the specified file path. It initializes the database
// with required buckets and returns an error if the database cannot be opened or initialized. The
// database file is created with 0600 permissions if it doesn't exist.
func NewBoltDB(path string) (BoltDB, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return BoltDB{}, fmt.Errorf("failed to open bolt db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("chats"))
		return err
	})

	return BoltDB{db: db}, err
}

func messageBucketName(chatID string) []byte {
	return []byte(fmt.Sprintf("chat-%s", chatID))
}

// Chats retrieves all stored chat records from the database in reverse chronological order. It
// returns a slice of Chat models or an error if the database operation fails.
func (b BoltDB) Chats(context.Context) ([]models.Chat, error) {
	var chats []models.Chat
	err := b.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("chats"))
		if b == nil {
			return nil
		}

		return b.ForEach(func(_, v []byte) error {
			var chat models.Chat
			if err := json.Unmarshal(v, &chat); err != nil {
				return fmt.Errorf("failed to unmarshal chat: %w", err)
			}
			chats = append(chats, chat)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	slices.Reverse(chats)
	return chats, nil
}

// AddChat stores a new chat record in the database and creates an associated message bucket. It
// generates a unique ID for the chat by combining a sequence number with the chat's original ID,
// and returns the new ID or an error if the operation fails.
func (b BoltDB) AddChat(_ context.Context, chat models.Chat) (string, error) {
	var newID string
	err := b.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("chats"))
		if b == nil {
			return nil
		}

		idPrefix, err := b.NextSequence()
		if err != nil {
			return fmt.Errorf("failed to get next sequence: %w", err)
		}
		newID = fmt.Sprintf("%d-%s", idPrefix, chat.ID)
		chat.ID = newID

		_, err = tx.CreateBucketIfNotExists(messageBucketName(chat.ID))
		if err != nil {
			return fmt.Errorf("failed to create message bucket: %w", err)
		}

		v, err := json.Marshal(chat)
		if err != nil {
			return fmt.Errorf("failed to marshal chat: %w", err)
		}

		return b.Put([]byte(newID), v)
	})

	return newID, err
}

// UpdateChat modifies an existing chat record in the database. If the chat doesn't exist, the
// operation is silently ignored. Returns an error if the marshaling or database operation fails.
func (b BoltDB) UpdateChat(_ context.Context, chat models.Chat) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("chats"))
		if b == nil {
			return nil
		}

		v := b.Get([]byte(chat.ID))
		if v == nil {
			return nil
		}

		v, err := json.Marshal(chat)
		if err != nil {
			return fmt.Errorf("failed to marshal chat: %w", err)
		}

		return b.Put([]byte(chat.ID), v)
	})
}

// Messages retrieves all messages associated with the specified chat ID. It returns the messages
// in their stored order or an error if the database operation fails.
func (b BoltDB) Messages(_ context.Context, chatID string) ([]models.Message, error) {
	var messages []models.Message
	err := b.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(messageBucketName(chatID))
		if b == nil {
			return nil
		}

		return b.ForEach(func(_, v []byte) error {
			var message models.Message
			if err := json.Unmarshal(v, &message); err != nil {
				return fmt.Errorf("failed to unmarshal message: %w", err)
			}
			messages = append(messages, message)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// AddMessage stores a new message in the specified chat's message bucket. It generates a unique
// ID for the message by combining a sequence number with the message's original ID, and returns
// the new ID or an error if the operation fails.
func (b BoltDB) AddMessage(_ context.Context, chatID string, message models.Message) (string, error) {
	var newID string
	err := b.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(messageBucketName(chatID))
		if b == nil {
			return nil
		}

		idPrefix, err := b.NextSequence()
		if err != nil {
			return fmt.Errorf("failed to get next sequence: %w", err)
		}
		newID = fmt.Sprintf("%d-%s", idPrefix, message.ID)
		message.ID = fmt.Sprintf("%d-%s", idPrefix, message.ID)

		v, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}

		return b.Put([]byte(newID), v)
	})

	return newID, err
}

// UpdateMessage modifies an existing message in the specified chat's message bucket. If the
// message doesn't exist, the operation is silently ignored. Returns an error if the marshaling
// or database operation fails.
func (b BoltDB) UpdateMessage(_ context.Context, chatID string, message models.Message) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(messageBucketName(chatID))
		if b == nil {
			return nil
		}

		msgID := message.ID

		v, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}

		return b.Put([]byte(msgID), v)
	})
}
