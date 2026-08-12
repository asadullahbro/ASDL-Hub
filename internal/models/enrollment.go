package models

import "time"

type EnrollmentToken struct {
	ID        string     `gorm:"primaryKey;size:36" json:"id"`
	Token     string     `gorm:"uniqueIndex;size:64;not null" json:"token"`
	Label     string     `gorm:"size:100" json:"label"`
	Used      bool       `gorm:"default:false" json:"used"`
	UsedBy    string     `gorm:"size:36" json:"used_by"` // node ID
	CreatedBy string     `gorm:"size:36" json:"created_by"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	UsedAt    *time.Time `json:"used_at"`
}

type WireGuardPeer struct {
	ID         string    `gorm:"primaryKey;size:36" json:"id"`
	NodeID     string    `gorm:"uniqueIndex;size:36;not null" json:"node_id"`
	PublicKey  string    `gorm:"uniqueIndex;size:100;not null" json:"public_key"`
	AssignedIP string    `gorm:"uniqueIndex;size:20;not null" json:"assigned_ip"`
	CreatedAt  time.Time `json:"created_at"`
}

type NodeSSHKey struct {
	ID         string    `gorm:"primaryKey;size:36" json:"id"`
	NodeID     string    `gorm:"uniqueIndex;size:36;not null" json:"node_id"`
	PublicKey  string    `gorm:"type:text;not null" json:"public_key"`
	PrivateKey string    `gorm:"type:text;not null" json:"-"` // hub's private key, never exposed
	SSHUser    string    `gorm:"size:50;not null" json:"ssh_user"`
	SSHPort    int       `gorm:"default:22" json:"ssh_port"`
	CreatedAt  time.Time `json:"created_at"`
}