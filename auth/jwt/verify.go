package jwt

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	http_context "github.com/eolinker/goku-eosc/node/http-context"
	"math/big"
	"reflect"
	"strings"
	"time"
)

type jwtToken struct {
	Token        string
	Header_64    string
	Claims_64    string
	Signature_64 string
	Header       map[string]interface{}
	Claims       map[string]interface{}
	Signature    string
}

type signingMethod struct {
	Name      string
	Hash      crypto.Hash
	KeySize   int
	CurveBits int
}

var (
	ErrInvalidKey           = errors.New("key is invalid")
	ErrInvalidKeyType       = errors.New("key is of invalid type")
	ErrHashUnavailable      = errors.New("the requested hash function is unavailable")
	ErrSignatureInvalid     = errors.New("signature is invalid")
	ErrInvalidSigningMethod = errors.New("signing method is invalid")
	ErrECDSAVerification    = errors.New("crypto/ecdsa: verification error")
	ErrKeyMustBePEMEncoded  = errors.New("Invalid Key: Key must be PEM encoded PKCS1 or PKCS8 private key")
	ErrNotRSAPublicKey      = errors.New("Key is not a valid RSA public key")
	ErrNotECPublicKey       = errors.New("Key is not a valid ECDSA public key")
)

func newSigningMethod(name string) *signingMethod {
	switch name {
	case "HS256":
		return &signingMethod{Name: name, Hash: crypto.SHA256}
	case "HS384":
		return &signingMethod{Name: name, Hash: crypto.SHA384}
	case "HS512":
		return &signingMethod{Name: name, Hash: crypto.SHA512}
	case "RS256":
		return &signingMethod{Name: name, Hash: crypto.SHA256}
	case "RS384":
		return &signingMethod{Name: name, Hash: crypto.SHA384}
	case "RS512":
		return &signingMethod{Name: name, Hash: crypto.SHA512}
	case "ES256":
		return &signingMethod{Name: name, Hash: crypto.SHA256, KeySize: 32, CurveBits: 256}
	case "ES384":
		return &signingMethod{Name: name, Hash: crypto.SHA384, KeySize: 48, CurveBits: 384}
	case "ES512":
		return &signingMethod{Name: name, Hash: crypto.SHA512, KeySize: 66, CurveBits: 512}
	default:
		return nil
	}
}

func (m *signingMethod) Verify(signingString, signature string, key interface{}) error {
	switch m.Name {
	case "HS256", "HS384", "HS512":
		{
			// Verify the key is the right type
			keyBytes, ok := key.([]byte)
			if !ok {
				return ErrInvalidKeyType
			}

			// Decode signature, for comparison
			sig, err := decodeSegment(signature)
			if err != nil {
				return err
			}

			// Can we use the specified hashing method?
			if !m.Hash.Available() {
				return ErrHashUnavailable
			}

			// This signing method is symmetric, so we validate the signature
			// by reproducing the signature from the signing string and key, then
			// comparing that against the provided signature.
			hasher := hmac.New(m.Hash.New, keyBytes)
			hasher.Write([]byte(signingString))
			if !hmac.Equal(sig, hasher.Sum(nil)) {
				return ErrSignatureInvalid
			}

			// No validation errors.  Signature is good.
			return nil
		}
	case "RS256", "RS384", "RS512":
		{
			var err error

			// Decode the signature
			var sig []byte
			if sig, err = decodeSegment(signature); err != nil {
				return err
			}

			var rsaKey *rsa.PublicKey
			var ok bool

			if rsaKey, ok = key.(*rsa.PublicKey); !ok {
				return ErrInvalidKeyType
			}

			// Create hasher
			if !m.Hash.Available() {
				return ErrHashUnavailable
			}
			hasher := m.Hash.New()
			hasher.Write([]byte(signingString))

			// Verify the signature
			return rsa.VerifyPKCS1v15(rsaKey, m.Hash, hasher.Sum(nil), sig)
		}
	case "ES256", "ES384", "ES512":
		{
			var err error

			// Decode the signature
			var sig []byte
			if sig, err = decodeSegment(signature); err != nil {
				return err
			}

			// Get the key
			var ecdsaKey *ecdsa.PublicKey
			switch k := key.(type) {
			case *ecdsa.PublicKey:
				ecdsaKey = k
			default:
				return ErrInvalidKeyType
			}

			if len(sig) != 2*m.KeySize {
				return ErrECDSAVerification
			}

			r := big.NewInt(0).SetBytes(sig[:m.KeySize])
			s := big.NewInt(0).SetBytes(sig[m.KeySize:])

			// Create hasher
			if !m.Hash.Available() {
				return ErrHashUnavailable
			}
			hasher := m.Hash.New()
			hasher.Write([]byte(signingString))

			// Verify the signature
			if verifystatus := ecdsa.Verify(ecdsaKey, hasher.Sum(nil), r, s); verifystatus == true {
				return nil
			} else {
				return ErrECDSAVerification
			}
		}
	default:
		{
			return ErrInvalidSigningMethod
		}
	}
}

func (m *signingMethod) Sign(signingString string, key interface{}) (string, error) {
	switch m.Name {
	case "HS256", "HS384", "HS512":
		{
			if keyBytes, ok := key.([]byte); ok {
				if !m.Hash.Available() {
					return "", ErrHashUnavailable
				}

				hasher := hmac.New(m.Hash.New, keyBytes)
				hasher.Write([]byte(signingString))

				return encodeSegment(hasher.Sum(nil)), nil
			}

			return "", ErrInvalidKeyType
		}
	case "RS256", "RS384", "RS512":
		{
			var rsaKey *rsa.PrivateKey
			var ok bool

			// Validate type of key
			if rsaKey, ok = key.(*rsa.PrivateKey); !ok {
				return "", ErrInvalidKey
			}

			// Create the hasher
			if !m.Hash.Available() {
				return "", ErrHashUnavailable
			}

			hasher := m.Hash.New()
			hasher.Write([]byte(signingString))

			// Sign the string and return the encoded bytes
			if sigBytes, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, m.Hash, hasher.Sum(nil)); err == nil {
				return encodeSegment(sigBytes), nil
			} else {
				return "", err
			}
		}
	case "ES256", "ES384", "ES512":
		{
			// Get the key
			var ecdsaKey *ecdsa.PrivateKey
			switch k := key.(type) {
			case *ecdsa.PrivateKey:
				ecdsaKey = k
			default:
				return "", ErrInvalidKeyType
			}

			// Create the hasher
			if !m.Hash.Available() {
				return "", ErrHashUnavailable
			}

			hasher := m.Hash.New()
			hasher.Write([]byte(signingString))

			// Sign the string and return r, s
			if r, s, err := ecdsa.Sign(rand.Reader, ecdsaKey, hasher.Sum(nil)); err == nil {
				curveBits := ecdsaKey.Curve.Params().BitSize

				if m.CurveBits != curveBits {
					return "", ErrInvalidKey
				}

				keyBytes := curveBits / 8
				if curveBits%8 > 0 {
					keyBytes += 1
				}

				// We serialize the outpus (r and s) into big-endian byte arrays and pad
				// them with zeros on the left to make sure the sizes work out. Both arrays
				// must be keyBytes long, and the output must be 2*keyBytes long.
				rBytes := r.Bytes()
				rBytesPadded := make([]byte, keyBytes)
				copy(rBytesPadded[keyBytes-len(rBytes):], rBytes)

				sBytes := s.Bytes()
				sBytesPadded := make([]byte, keyBytes)
				copy(sBytesPadded[keyBytes-len(sBytes):], sBytes)

				out := append(rBytesPadded, sBytesPadded...)

				return encodeSegment(out), nil
			} else {
				return "", err
			}
		}
	default:
		{
			return "", ErrInvalidSigningMethod
		}
	}
}

func methodEnable(method string) bool {
	if method == "HS256" || method == "HS384" || method == "HS512" || method == "RS256" || method == "RS384" || method == "RS512" || method == "ES256" || method == "ES384" || method == "ES512" {
		return true
	}
	return false
}

// Decode JWT specific base64url encoding with padding stripped
func decodeSegment(seg string) ([]byte, error) {
	if l := len(seg) % 4; l > 0 {
		seg += strings.Repeat("=", 4-l)
	}

	return base64.URLEncoding.DecodeString(seg)
}

// Encode JWT specific base64url encoding with padding stripped
func encodeSegment(seg []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(seg), "=")
}

// Parse PEM encoded PKCS1 or PKCS8 public key
func ParseRSAPublicKeyFromPEM(key []byte) (*rsa.PublicKey, error) {
	var err error

	// Parse PEM block
	var block *pem.Block
	if block, _ = pem.Decode(key); block == nil {
		return nil, ErrKeyMustBePEMEncoded
	}

	// Parse the key
	var parsedKey interface{}
	if parsedKey, err = x509.ParsePKIXPublicKey(block.Bytes); err != nil {
		if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
			parsedKey = cert.PublicKey
		} else {
			return nil, err
		}
	}

	var pkey *rsa.PublicKey
	var ok bool
	if pkey, ok = parsedKey.(*rsa.PublicKey); !ok {
		return nil, ErrNotRSAPublicKey
	}

	return pkey, nil
}

// Parse PEM encoded PKCS1 or PKCS8 public key
func ParseECPublicKeyFromPEM(key []byte) (*ecdsa.PublicKey, error) {
	var err error

	// Parse PEM block
	var block *pem.Block
	if block, _ = pem.Decode(key); block == nil {
		return nil, ErrKeyMustBePEMEncoded
	}

	// Parse the key
	var parsedKey interface{}
	if parsedKey, err = x509.ParsePKIXPublicKey(block.Bytes); err != nil {
		if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
			parsedKey = cert.PublicKey
		} else {
			return nil, err
		}
	}

	var pkey *ecdsa.PublicKey
	var ok bool
	if pkey, ok = parsedKey.(*ecdsa.PublicKey); !ok {
		return nil, ErrNotECPublicKey
	}

	return pkey, nil
}

// base64解密
func b64Decode(input string) (string, error) {
	remainder := len(input) % 4
	// base64编码需要为4的倍数，如果不是4的倍数，则填充"="号
	if remainder > 0 {
		padlen := 4 - remainder
		input = input + strings.Repeat("=", padlen)
	}
	// 将原字符串中的"_","-"分别用"/"和"+"替换
	input = strings.Replace(strings.Replace(input, "_", "/", -1), "-", "+", -1)
	result, err := base64.StdEncoding.DecodeString(input)
	return string(result), err
}

// 根据"."分割token字符串
func tokenize(token string) []string {
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		return parts
	} else {
		return nil
	}
}

// 解析token，将token信息解析为jwtToken对象
func decodeToken(token string) (*jwtToken, error) {
	tokenParts := tokenize(token)
	if tokenParts == nil {
		return nil, errors.New("[jwt_auth] Invalid token")
	}
	header_64 := tokenParts[0]
	claims_64 := tokenParts[1]
	signature_64 := tokenParts[2]
	var header, claims map[string]interface{}
	var signature string
	header_d64, err := b64Decode(header_64)
	if err != nil {
		return nil, errors.New("[jwt_auth] Invalid base64 encoded JSON")
	}

	if err = json.Unmarshal([]byte(header_d64), &header); err != nil {
		return nil, errors.New("[jwt_auth] Invalid JSON")
	}
	claims_d64, err := b64Decode(claims_64)
	if err != nil {
		return nil, errors.New("[jwt_auth] Invalid base64 encoded JSON")
	}
	if err = json.Unmarshal([]byte(claims_d64), &claims); err != nil {
		return nil, errors.New("[jwt_auth] Invalid JSON")
	}
	signature, err = b64Decode(signature_64)
	if err != nil {
		return nil, errors.New("[jwt_auth] Invalid base64 encoded JSON")
	}
	if _, ok := header["typ"]; !ok || strings.ToUpper(header["typ"].(string)) != "JWT" {
		return nil, errors.New("[jwt_auth] Invalid typ")
	}
	if _, ok := header["alg"]; !ok || !methodEnable(header["alg"].(string)) {
		return nil, errors.New("[jwt_auth] Invalid alg")
	}
	if len(claims) == 0 {
		return nil, errors.New("[jwt_auth] Invalid claims")
	}
	if len(signature) == 0 {
		return nil, errors.New("[jwt_auth] Invalid signature")
	}
	return &jwtToken{Token: token, Header_64: header_64, Claims_64: claims_64, Signature_64: signature_64, Header: header, Claims: claims, Signature: signature}, nil
}

//verifySignature 验证签名
func verifySignature(token *jwtToken, key string) error {

	var k interface{}
	switch token.Header["alg"].(string) {
	case "HS256", "HS384", "HS512":
		{
			k = []byte(key)
		}
	case "RS256", "RS384", "RS512":
		{
			var err error
			k, err = ParseRSAPublicKeyFromPEM([]byte(key))
			if err != nil {
				return err
			}
		}
	case "ES256", "ES384", "ES512":
		{
			var err error
			k, err = ParseECPublicKeyFromPEM([]byte(key))
			if err != nil {
				return err
			}
		}
	default:
		{
			return ErrInvalidSigningMethod
		}
	}
	return newSigningMethod(token.Header["alg"].(string)).Verify(token.Header_64+"."+token.Claims_64, token.Signature_64, k)
}

//verifyRegisteredClaims 验证签发字段
func verifyRegisteredClaims(token *jwtToken, claimsToVerify []string) error {
	if claimsToVerify == nil {
		claimsToVerify = []string{}
	}

	for _, claimName := range claimsToVerify {
		var claim int64 = 0
		if _, ok := token.Claims[claimName]; ok {

			if typeOfData(token.Claims[claimName]) == reflect.Float64 {
				claimFloat64, success := token.Claims[claimName].(float64)
				if success {
					claim = int64(claimFloat64)
				}
			}

		}
		if claim < 1 {
			return errors.New("[jwt_auth] " + claimName + " must be a number")
		}
		switch claimName {
		case "nbf":
			if claim > time.Now().Unix() {
				return errors.New("[jwt_auth] token not valid yet")
			}
		case "exp":
			if claim <= time.Now().Unix() {
				return errors.New("[jwt_auth] token expired")
			}
		default:
			return errors.New("[jwt_auth] Invalid claims")
		}
	}
	return nil
}

//获取数据的类型
func typeOfData(data interface{}) reflect.Kind {
	value := reflect.ValueOf(data)
	valueType := value.Kind()
	if valueType == reflect.Ptr {
		valueType = value.Elem().Kind()
	}
	return valueType
}

//retrieveJWTToken 获取jwtToken字符串
func (j *jwt) retrieveJWTToken(context *http_context.Context) (string, error) {
	const tokenName = "jwt_token"
	if authorizationHeader := context.Request().GetHeader("Authorization"); authorizationHeader != "" {
		if j.hideCredentials {
			context.Proxy().DelHeader("Authorization")
		}
		if strings.Contains(authorizationHeader, "bearer ") {
			authorizationHeader = authorizationHeader[7:]
		}
		return authorizationHeader, nil
	}

	if value, ok := context.Request().URL().Query()[tokenName]; ok {
		if j.hideCredentials {
			context.Proxy().Querys().Del(tokenName)
		}
		return value[0], nil
	}

	formdata, err := context.Request().BodyForm()
	if err != nil {
		return "", errors.New("[jwt_auth] cannot find token in request")
	}
	if value, ok := formdata[tokenName]; ok {
		if j.hideCredentials {
			delete(formdata, tokenName)
			context.Proxy().SetForm(formdata)
		}
		return value[0], nil
	}
	return "", errors.New("[jwt_auth] cannot find token in request")
}

//doJWTAuthentication 进行JWT鉴权
func (j *jwt) doJWTAuthentication(context *http_context.Context) error {
	tokenStr, err := j.retrieveJWTToken(context)
	if err != nil {
		return errors.New("[jwt_auth] Unrecognizable token")
	}
	token, err := decodeToken(tokenStr)
	if err != nil {
		return errors.New("[jwt_auth] Bad token; " + err.Error())
	}

	key := ""
	keyClaimName := "iss"
	if _, ok := token.Claims[keyClaimName]; ok {
		key = token.Claims[keyClaimName].(string)
	} else if _, ok = token.Header[keyClaimName]; ok {
		key = token.Header[keyClaimName].(string)
	}

	if key == "" {
		return errors.New("[jwt_auth] No mandatory " + keyClaimName + " in claims")
	}

	// 从配置中获取jwt凭证配置
	issAlgorithm := fmt.Sprintf("%s%s", key, token.Header["alg"].(string))
	jwtSecret, has := j.credentials[issAlgorithm]
	if !has {
		return errors.New("[jwt_auth] No credentials found for given " + keyClaimName)
	}

	jwtSecretValue := jwtSecret.RSAPublicKey
	algorithm := "HS256"
	if jwtSecret.Algorithm != "" {
		algorithm = jwtSecret.Algorithm
	}
	if algorithm == "HS256" || algorithm == "HS384" || algorithm == "HS512" {
		jwtSecretValue = jwtSecret.Secret
	}
	if j.signatureIsBase64 {
		jwtSecretValue, err = b64Decode(jwtSecretValue)
		if err != nil {
			return errors.New("[jwt_auth] Invalid key/secret")
		}
	}
	if jwtSecretValue == "" {
		return errors.New("[jwt_auth] Invalid key/secret")
	}

	if err = verifySignature(token, jwtSecretValue); err != nil {
		return errors.New("[jwt_auth] Invalid signature")
	}
	if err = verifyRegisteredClaims(token, j.claimsToVerify); err != nil {
		return err
	}
	return nil
}

func validateCredentials(credentials []JwtCredential) (map[string]*JwtCredential, error) {
	IssAlgorithmMap := map[string]*JwtCredential{}

	for k, credential := range credentials {
		s := fmt.Sprintf("%s%s", credential.Iss, credential.Algorithm)
		if _, has := IssAlgorithmMap[s]; has {
			return nil, fmt.Errorf("[jwt_auth] the combine of Iss and Algorithm Repeat")
		}
		IssAlgorithmMap[s] = &credentials[k]
	}

	return IssAlgorithmMap, nil
}
