package wecom

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

const blockSize = 32

func computeSignature(
	token, timestamp, nonce, encrypt string,
) string {
	params := []string{token, timestamp, nonce, encrypt}
	sort.Strings(params)
	str := strings.Join(params, "")
	hash := sha1.Sum([]byte(str))
	return fmt.Sprintf("%x", hash)
}

func verifySignature(
	token, msgSignature, timestamp, nonce, msgEncrypt string,
) bool {
	if token == "" {
		return false
	}
	return computeSignature(
		token, timestamp, nonce, msgEncrypt,
	) == msgSignature
}

func decryptMessage(
	encryptedMsg, encodingAESKey string,
) (string, error) {
	return decryptMessageWithVerify(
		encryptedMsg, encodingAESKey, "",
	)
}

func decryptMessageWithVerify(
	encryptedMsg, encodingAESKey, receiveid string,
) (string, error) {
	if encodingAESKey == "" {
		decoded, err := base64.StdEncoding.DecodeString(
			encryptedMsg,
		)
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}

	aesKey, err := decodeWeComAESKey(encodingAESKey)
	if err != nil {
		return "", err
	}

	cipherText, err := base64.StdEncoding.DecodeString(
		encryptedMsg,
	)
	if err != nil {
		return "", fmt.Errorf(
			"failed to decode message: %w", err,
		)
	}

	plainText, err := decryptAESCBC(aesKey, cipherText)
	if err != nil {
		return "", err
	}

	return unpackWeComFrame(plainText, receiveid)
}

func decodeWeComAESKey(
	encodingAESKey string,
) ([]byte, error) {
	aesKey, err := base64.StdEncoding.DecodeString(
		encodingAESKey + "=",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to decode AES key: %w", err,
		)
	}
	if len(aesKey) != 32 {
		return nil, fmt.Errorf(
			"invalid AES key length: %d", len(aesKey),
		)
	}
	return aesKey, nil
}

func encryptAESCBC(
	aesKey, plaintext []byte,
) ([]byte, error) {
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create cipher: %w", err,
		)
	}
	iv := aesKey[:aes.BlockSize]
	ciphertext := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(
		ciphertext, plaintext,
	)
	return ciphertext, nil
}

func packWeComFrame(
	msg, receiveid string,
) ([]byte, error) {
	randomBytes := make([]byte, 16)
	for i := range 16 {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return nil, fmt.Errorf(
				"failed to generate random: %w", err,
			)
		}
		randomBytes[i] = byte('0' + n.Int64())
	}
	msgBytes := []byte(msg)
	msgLenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(
		msgLenBytes, uint32(len(msgBytes)),
	)
	var buf bytes.Buffer
	buf.Write(randomBytes)
	buf.Write(msgLenBytes)
	buf.Write(msgBytes)
	buf.WriteString(receiveid)
	return buf.Bytes(), nil
}

func unpackWeComFrame(
	data []byte, receiveid string,
) (string, error) {
	if len(data) < 20 {
		return "", fmt.Errorf(
			"decrypted frame too short: %d bytes", len(data),
		)
	}
	msgLen := binary.BigEndian.Uint32(data[16:20])
	if int(msgLen) > len(data)-20 {
		return "", fmt.Errorf(
			"invalid message length: %d", msgLen,
		)
	}
	msg := data[20 : 20+msgLen]
	if receiveid != "" && len(data) > 20+int(msgLen) {
		actualReceiveID := string(data[20+msgLen:])
		if actualReceiveID != receiveid {
			return "", fmt.Errorf(
				"receiveid mismatch: expected %s, got %s",
				receiveid, actualReceiveID,
			)
		}
	}
	return string(msg), nil
}

func decryptAESCBC(
	aesKey, ciphertext []byte,
) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("ciphertext is empty")
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf(
			"ciphertext length %d not multiple of block size",
			len(ciphertext),
		)
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create cipher: %w", err,
		)
	}
	iv := aesKey[:aes.BlockSize]
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(
		plaintext, ciphertext,
	)
	plaintext, err = pkcs7Unpad(plaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to unpad: %w", err)
	}
	return plaintext, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	if padding == 0 {
		padding = blockSize
	}
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize {
		return nil, fmt.Errorf(
			"invalid padding size: %d", padding,
		)
	}
	if padding > len(data) {
		return nil, fmt.Errorf(
			"padding size larger than data",
		)
	}
	for i := range padding {
		if data[len(data)-1-i] != byte(padding) {
			return nil, fmt.Errorf(
				"invalid padding byte at position %d", i,
			)
		}
	}
	return data[:len(data)-padding], nil
}
