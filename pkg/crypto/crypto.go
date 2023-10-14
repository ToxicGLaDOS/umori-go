package crypto

import (
	"errors"
	"encoding/base64"
	"fmt"
	"strings"
  "crypto/rand"
  "crypto/subtle"
	"golang.org/x/crypto/argon2"
)

// Thanks to https://www.alexedwards.net/blog/how-to-hash-and-verify-passwords-with-argon2-in-go
// for this code
type HashingParams struct {
    memory      uint32
    iterations  uint32
    parallelism uint8
    saltLength  uint32
    keyLength   uint32
}

var (
    ErrInvalidHash         = errors.New("the encoded hash is not in the correct format")
    ErrIncompatibleVersion = errors.New("incompatible version of argon2")
)

func DefaultHashingParams() *HashingParams{
    return &HashingParams{
        memory:      64 * 1024,
        iterations:  3,
        parallelism: 2,
        saltLength:  16,
        keyLength:   32,
    }
}

func generateRandomBytes(n uint32) ([]byte, error) {
    b := make([]byte, n)
    _, err := rand.Read(b)
    if err != nil {
        return nil, err
    }

    return b, nil
}

func DecodeHash(encodedHash string) (p *HashingParams, salt, hash []byte, err error) {
    vals := strings.Split(encodedHash, "$")
    if len(vals) != 6 {
        return nil, nil, nil, ErrInvalidHash
    }

    var version int
    _, err = fmt.Sscanf(vals[2], "v=%d", &version)
    if err != nil {
        return nil, nil, nil, err
    }
    if version != argon2.Version {
        return nil, nil, nil, ErrIncompatibleVersion
    }

    p = &HashingParams{}
    _, err = fmt.Sscanf(vals[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism)
    if err != nil {
        return nil, nil, nil, err
    }

    salt, err = base64.RawStdEncoding.Strict().DecodeString(vals[4])
    if err != nil {
        return nil, nil, nil, err
    }
    p.saltLength = uint32(len(salt))

    hash, err = base64.RawStdEncoding.Strict().DecodeString(vals[5])
    if err != nil {
        return nil, nil, nil, err
    }
    p.keyLength = uint32(len(hash))

    return p, salt, hash, nil
}

func ComparePasswordAndHash(password, encodedHash string) (match bool, err error) {
    // Extract the parameters, salt and derived key from the encoded password
    // hash.
    p, salt, hash, err := DecodeHash(encodedHash)
    if err != nil {
        return false, err
    }

    // Derive the key from the other password using the same parameters.
    otherHash := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)

    // Check that the contents of the hashed passwords are identical. Note
    // that we are using the subtle.ConstantTimeCompare() function for this
    // to help prevent timing attacks.
    if subtle.ConstantTimeCompare(hash, otherHash) == 1 {
        return true, nil
    }
    return false, nil
}

func GenerateFromPassword(password string, p *HashingParams) (encodedHash string, err error) {
    salt, err := generateRandomBytes(p.saltLength)
    if err != nil {
        return "", err
    }

    hash := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)

    // Base64 encode the salt and hashed password.
    b64Salt := base64.RawStdEncoding.EncodeToString(salt)
    b64Hash := base64.RawStdEncoding.EncodeToString(hash)

    // Return a string using the standard encoded hash representation.
    encodedHash = fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.memory, p.iterations, p.parallelism, b64Salt, b64Hash)

    return encodedHash, nil
}
