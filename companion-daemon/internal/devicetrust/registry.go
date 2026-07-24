package devicetrust

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

const deviceRecordVersion = 1

// Role values. First paired device gets owner; further devices are members.
// Full role/permission UI is future work; the field exists now.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

// Device is a paired-device record. Public keys only — never a phone private
// key. DisplayName is untrusted presentation metadata.
//
// Epoch is a monotonically increasing counter incremented on every Revoke,
// RecoverOwner, or authority change. Every issued bearer/session is bound to
// (deviceID, epoch, hostID, bootID). A stale epoch at authentication time
// means the device was revoked or replaced — the bearer is rejected.
type Device struct {
	Version      int        `json:"version"`
	DeviceID     string     `json:"deviceId"`
	PublicKeyDER []byte     `json:"publicKey"`
	Fingerprint  string     `json:"fingerprint"`
	DisplayName  string     `json:"displayName"`
	Role         string     `json:"role"`
	Epoch        int64      `json:"epoch"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastSeenAt   time.Time  `json:"lastSeenAt"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
}

// Revoked reports whether the device has been revoked.
func (d Device) Revoked() bool { return d.RevokedAt != nil }

// PublicDevice is the safe view for API/UI: identity + presentation, no raw key
// material beyond the (public) fingerprint.
type PublicDevice struct {
	DeviceID    string     `json:"deviceId"`
	Fingerprint string     `json:"fingerprint"`
	DisplayName string     `json:"displayName"`
	Role        string     `json:"role"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
}

// Public returns the API/UI-safe device view.
func (d Device) Public() PublicDevice {
	var rev *time.Time
	if d.RevokedAt != nil {
		t := *d.RevokedAt
		rev = &t
	}
	return PublicDevice{
		DeviceID:    d.DeviceID,
		Fingerprint: d.Fingerprint,
		DisplayName: d.DisplayName,
		Role:        d.Role,
		CreatedAt:   d.CreatedAt,
		LastSeenAt:  d.LastSeenAt,
		RevokedAt:   rev,
	}
}

// DeviceStore persists the device registry. FileDeviceStore is the MVP.
type DeviceStore interface {
	// Load returns the persisted devices, or an empty slice if none exist. A
	// corrupt store returns an error (fail closed) rather than an empty set.
	Load() ([]Device, error)
	Save([]Device) error
}

// DeviceRegistry manages paired devices with atomic owner-only persistence.
type DeviceRegistry struct {
	mu      sync.Mutex
	store   DeviceStore
	devices map[string]*Device
	order   []string
}

// cloneDevice returns a deep copy so callers and the store never share the
// registry's internal mutable slice/pointer fields (PublicKeyDER, RevokedAt).
func cloneDevice(d Device) Device {
	c := d
	if d.PublicKeyDER != nil {
		c.PublicKeyDER = append([]byte(nil), d.PublicKeyDER...)
	}
	if d.RevokedAt != nil {
		t := *d.RevokedAt
		c.RevokedAt = &t
	}
	return c
}

// validateDeviceRecord fails closed on any inconsistency in a persisted record.
func validateDeviceRecord(d Device) error {
	if d.Version != deviceRecordVersion {
		return fmt.Errorf("%w: unsupported device record version %d", ErrCorruptStore, d.Version)
	}
	if _, err := ParseP256PublicKey(d.PublicKeyDER); err != nil {
		return fmt.Errorf("%w: device %s has a non-P-256 key", ErrCorruptStore, d.DeviceID)
	}
	fp := Fingerprint(d.PublicKeyDER)
	if d.Fingerprint != fp {
		return fmt.Errorf("%w: device %s fingerprint mismatch", ErrCorruptStore, d.DeviceID)
	}
	if d.DeviceID != fp {
		return fmt.Errorf("%w: device id %s does not match key fingerprint", ErrCorruptStore, d.DeviceID)
	}
	if d.Role != RoleOwner && d.Role != RoleMember {
		return fmt.Errorf("%w: device %s has invalid role %q", ErrCorruptStore, d.DeviceID, d.Role)
	}
	if d.CreatedAt.IsZero() || d.LastSeenAt.IsZero() {
		return fmt.Errorf("%w: device %s has invalid timestamps", ErrCorruptStore, d.DeviceID)
	}
	if d.RevokedAt != nil && d.RevokedAt.IsZero() {
		return fmt.Errorf("%w: device %s has invalid revokedAt", ErrCorruptStore, d.DeviceID)
	}
	return nil
}

// NewDeviceRegistry loads the persisted registry. Every record is validated for
// internal consistency (key↔fingerprint↔deviceId, role, timestamps, no
// duplicates); a corrupt/insecure store fails closed (error) rather than
// resetting trust to empty or silently dropping records.
func NewDeviceRegistry(store DeviceStore) (*DeviceRegistry, error) {
	recs, err := store.Load()
	if err != nil {
		return nil, err
	}
	r := &DeviceRegistry{store: store, devices: make(map[string]*Device)}
	for i := range recs {
		d := recs[i]
		if verr := validateDeviceRecord(d); verr != nil {
			return nil, verr
		}
		if _, dup := r.devices[d.DeviceID]; dup {
			return nil, fmt.Errorf("%w: duplicate device id %s", ErrCorruptStore, d.DeviceID)
		}
		cd := cloneDevice(d)
		r.devices[cd.DeviceID] = &cd
		r.order = append(r.order, cd.DeviceID)
	}
	return r, nil
}

// Add registers a device by its canonical P-256 public key. The device ID is
// derived from the fingerprint, so adding the same key twice is deterministic:
// an active duplicate returns the existing record; a revoked key is not
// resurrected (ErrDeviceRevoked). The first device paired becomes the owner.
func (r *DeviceRegistry) Add(pubDER []byte, displayName string) (Device, error) {
	if _, err := ParseP256PublicKey(pubDER); err != nil {
		return Device{}, err
	}
	fp := Fingerprint(pubDER)
	id := fp

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.devices[id]; ok {
		if existing.Revoked() {
			return cloneDevice(*existing), ErrDeviceRevoked
		}
		return cloneDevice(*existing), nil
	}

	role := RoleMember
	if r.countActiveLocked() == 0 {
		role = RoleOwner
	}
	now := time.Now().UTC()
	d := &Device{
		Version:      deviceRecordVersion,
		DeviceID:     id,
		PublicKeyDER: append([]byte(nil), pubDER...),
		Fingerprint:  fp,
		DisplayName:  displayName,
		Role:         role,
		CreatedAt:    now,
		LastSeenAt:   now,
	}
	r.devices[id] = d
	r.order = append(r.order, id)
	if err := r.saveLocked(); err != nil {
		delete(r.devices, id)
		r.order = r.order[:len(r.order)-1]
		return Device{}, err
	}
	return cloneDevice(*d), nil
}

// GetActive returns a non-revoked device by ID.
func (r *DeviceRegistry) GetActive(deviceID string) (Device, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[deviceID]
	if !ok || d.Revoked() {
		return Device{}, false
	}
	return cloneDevice(*d), true
}

// GetActiveByFingerprint returns a non-revoked device by public-key fingerprint
// (the authentication lookup used by later phases). Revoked keys never match.
func (r *DeviceRegistry) GetActiveByFingerprint(fp string) (Device, bool) {
	return r.GetActive(fp)
}

// List returns all devices (including revoked), in insertion order.
func (r *DeviceRegistry) List() []Device {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Device, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, cloneDevice(*r.devices[id]))
	}
	return out
}

// Revoke marks a device revoked, increments its epoch, and persists.
// The epoch bump invalidates every session/bearer issued under the old epoch.
// Idempotent. Unknown → error.
func (r *DeviceRegistry) Revoke(deviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[deviceID]
	if !ok {
		return ErrDeviceNotFound
	}
	if d.Revoked() {
		return nil
	}
	now := time.Now().UTC()
	d.RevokedAt = &now
	prevEpoch := d.Epoch
	d.Epoch++
	if err := r.saveLocked(); err != nil {
		d.RevokedAt = nil
		d.Epoch = prevEpoch
		return err
	}
	return nil
}

// GetAuth returns the authoritative device authorization state.
func (r *DeviceRegistry) GetAuth(deviceID string) AuthorizationState {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[deviceID]
	if !ok || d.Revoked() {
		return AuthorizationState{Active: false}
	}
	return AuthorizationState{Epoch: uint64(d.Epoch), Active: true}
}

// ── 9.4-D: Token-based epoch commit protocol ──
//
// ReserveEpoch issues a lightweight EpochToken (lock held briefly, then
// released). The caller performs mutation I/O with the token, then calls
// CommitEpoch to atomically re-validate the epoch against the current
// registry state. Revoke bumps the epoch → all in-flight tokens become
// invalid at commit time.
//
// This avoids holding the registry lock across I/O (provider delivery,
// process spawn, WebSocket writes).

// EpochToken is a lightweight epoch snapshot. It does NOT hold the registry
// lock — the lock is released before ReserveEpoch returns.
type EpochToken struct {
	DeviceID string
	Epoch    uint64
}

// ReserveEpoch atomically validates that the device is active and its current
// epoch matches expectedEpoch. On success returns a token (lock released);
// on failure the lock is released before returning.
// Nil registry → fail-closed (ErrStaleDevice).
func (reg *DeviceRegistry) ReserveEpoch(deviceID string, expectedEpoch uint64) (*EpochToken, error) {
	if reg == nil {
		return nil, fmt.Errorf("%w: no device registry configured", ErrStaleDevice)
	}
	reg.mu.Lock()
	d, ok := reg.devices[deviceID]
	if !ok || d.Revoked() {
		reg.mu.Unlock()
		return nil, fmt.Errorf("%w: device is not active", ErrStaleDevice)
	}
	if uint64(d.Epoch) != expectedEpoch {
		reg.mu.Unlock()
		return nil, fmt.Errorf("%w: expected %d, current %d", ErrStaleDevice, expectedEpoch, d.Epoch)
	}
	tok := &EpochToken{DeviceID: deviceID, Epoch: uint64(d.Epoch)}
	reg.mu.Unlock()
	return tok, nil
}

// CommitEpoch atomically re-validates the token against the current registry
// state. Returns nil if the epoch matches; ErrStaleDevice otherwise.
// Nil token → no-op (insecure-local path).
func (reg *DeviceRegistry) CommitEpoch(tok *EpochToken) error {
	if tok == nil {
		return nil
	}
	if reg == nil {
		return fmt.Errorf("%w: no device registry configured", ErrStaleDevice)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	d, ok := reg.devices[tok.DeviceID]
	if !ok || d.Revoked() {
		return fmt.Errorf("%w: device is not active", ErrStaleDevice)
	}
	if uint64(d.Epoch) != tok.Epoch {
		return fmt.Errorf("%w: epoch changed from %d to %d", ErrStaleDevice, tok.Epoch, d.Epoch)
	}
	return nil
}

// GetEpoch returns the current authorization epoch for a device.
// Returns 0 if the device is unknown (epoch 0 means never paired).
func (r *DeviceRegistry) GetEpoch(deviceID string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[deviceID]
	if !ok {
		return 0
	}
	return d.Epoch
}

// TouchLastSeen updates lastSeenAt for an active device and persists it.
func (r *DeviceRegistry) TouchLastSeen(deviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[deviceID]
	if !ok || d.Revoked() {
		return ErrDeviceNotFound
	}
	prev := d.LastSeenAt
	d.LastSeenAt = time.Now().UTC()
	if err := r.saveLocked(); err != nil {
		d.LastSeenAt = prev
		return err
	}
	return nil
}

// ErrNotOwner is returned when an operation requires the device to be the
// current owner but it is not (member, revoked, or unknown).
var ErrNotOwner = errors.New("device is not the current owner")

// ErrAlreadyOwner is returned when promoting a device that is already owner.
var ErrAlreadyOwner = errors.New("device is already owner")

// RecoverOwner atomically revokes oldOwnerID and promotes newOwnerID from
// member to owner. This is a host-local privilege available only through the
// Unix socket; it is never exposed over HTTP/WS/tunnel.
//
// Preconditions (fail-closed):
//   - oldOwnerID must be the current active owner (not revoked, not missing)
//   - newOwnerID must be an active member (not revoked, not missing, not already
//     owner)
//
// On success the old owner is revoked, the target is promoted to owner, the
// change is persisted atomically, and exactly one active owner exists. On any
// persistence failure neither mutation becomes durable (in-memory state is
// rolled back).
func (r *DeviceRegistry) RecoverOwner(oldOwnerID, newOwnerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	oldDev, ok := r.devices[oldOwnerID]
	if !ok {
		return fmt.Errorf("%w: old owner %s", ErrDeviceNotFound, oldOwnerID)
	}
	if oldDev.Revoked() {
		return fmt.Errorf("%w: old owner %s is already revoked", ErrDeviceRevoked, oldOwnerID)
	}
	if oldDev.Role != RoleOwner {
		return fmt.Errorf("%w: device %s has role %q", ErrNotOwner, oldOwnerID, oldDev.Role)
	}

	newDev, ok := r.devices[newOwnerID]
	if !ok {
		return fmt.Errorf("%w: target %s", ErrDeviceNotFound, newOwnerID)
	}
	if newDev.Revoked() {
		return fmt.Errorf("%w: target %s is revoked", ErrDeviceRevoked, newOwnerID)
	}
	if newDev.Role == RoleOwner {
		return fmt.Errorf("%w: target %s is already owner", ErrAlreadyOwner, newOwnerID)
	}

	// Atomic: revoke old + promote new + bump epochs. On save failure, roll back.
	now := time.Now().UTC()
	oldDev.RevokedAt = &now
	oldPrevEpoch := oldDev.Epoch
	oldDev.Epoch++
	oldDevRole := oldDev.Role
	newDev.Role = RoleOwner
	newPrevEpoch := newDev.Epoch
	newDev.Epoch++

	if err := r.saveLocked(); err != nil {
		// Roll back in-memory to match persisted state.
		oldDev.RevokedAt = nil
		oldDev.Epoch = oldPrevEpoch
		oldDev.Role = oldDevRole
		newDev.Role = RoleMember
		newDev.Epoch = newPrevEpoch
		return err
	}

	return nil
}

func (r *DeviceRegistry) countActiveLocked() int {
	n := 0
	for _, d := range r.devices {
		if !d.Revoked() {
			n++
		}
	}
	return n
}

func (r *DeviceRegistry) saveLocked() error {
	out := make([]Device, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, cloneDevice(*r.devices[id]))
	}
	return r.store.Save(out)
}

// FileDeviceStore is the MVP owner-only file DeviceStore.
type FileDeviceStore struct{ Path string }

func (f *FileDeviceStore) Load() ([]Device, error) {
	raw, err := readOwnerOnly(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no devices paired yet
		}
		return nil, err // insecure permissions → fail closed
	}
	var recs []Device
	if err := json.Unmarshal(raw, &recs); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptStore, err)
	}
	for i := range recs {
		if recs[i].Version != deviceRecordVersion {
			return nil, fmt.Errorf("%w: unsupported device record version %d", ErrCorruptStore, recs[i].Version)
		}
		if _, err := ParseP256PublicKey(recs[i].PublicKeyDER); err != nil {
			return nil, fmt.Errorf("%w: device %s has a non-P-256 key", ErrCorruptStore, recs[i].DeviceID)
		}
	}
	return recs, nil
}

func (f *FileDeviceStore) Save(recs []Device) error {
	raw, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	return writeOwnerOnly(f.Path, raw)
}
