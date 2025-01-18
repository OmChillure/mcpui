package services

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/MegaGrindStone/mcp-web-ui/internal/models"
	bolt "go.etcd.io/bbolt"
)

type BoltDB struct {
	db *bolt.DB
}

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

func (b BoltDB) Chats(ctx context.Context) ([]models.Chat, error) {
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

func (b BoltDB) AddChat(ctx context.Context, chat models.Chat) (string, error) {
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

func (b BoltDB) SetChatTitle(ctx context.Context, chatID string, title string) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("chats"))
		if b == nil {
			return nil
		}

		v := b.Get([]byte(chatID))
		if v == nil {
			return nil
		}

		var chat models.Chat
		if err := json.Unmarshal(v, &chat); err != nil {
			return fmt.Errorf("failed to unmarshal chat: %w", err)
		}
		chat.Title = title

		v, err := json.Marshal(chat)
		if err != nil {
			return fmt.Errorf("failed to marshal chat: %w", err)
		}

		return b.Put([]byte(chatID), v)
	})
}

func (b BoltDB) Messages(ctx context.Context, chatID string) ([]models.Message, error) {
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

func (b BoltDB) AddMessages(ctx context.Context, chatID string, messages []models.Message) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(messageBucketName(chatID))
		if b == nil {
			return nil
		}

		for _, message := range messages {
			idPrefix, err := b.NextSequence()
			if err != nil {
				return fmt.Errorf("failed to get next sequence: %w", err)
			}
			message.ID = fmt.Sprintf("%d-%s", idPrefix, message.ID)

			v, err := json.Marshal(message)
			if err != nil {
				return fmt.Errorf("failed to marshal message: %w", err)
			}

			if err := b.Put([]byte(message.ID), v); err != nil {
				return fmt.Errorf("failed to put message: %w", err)
			}
		}
		return nil
	})
}

func (b BoltDB) UpdateMessage(ctx context.Context, chatID string, message models.Message) error {
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
