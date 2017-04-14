package gossip

import "gopkg.in/dedis/crypto.v0/abstract"

func Hash(suite abstract.Suite, x abstract.Scalar, msg []byte) ([]byte, *DLEQProof, error) {
	g := suite.Point().Base()
	H := hashToPoint(suite, msg)
	pr, _, xH, err := NewDLEQProof(suite, g, H, x)
	if err != nil {
		return nil, nil, err
	}
	buff, err := xH.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}
	return buff, pr, nil
}

func Verify(suite abstract.Suite, public abstract.Point, msg, sum []byte, proof *DLEQProof) bool {
	g := suite.Point().Base()
	xG := public
	xH := suite.Point()
	if err := xH.UnmarshalBinary(sum); err != nil {
		return false
	}
	H := hashToPoint(suite, msg)
	if err := proof.Verify(suite, g, H, xG, xH); err != nil {
		return false
	}
	return true
}

// H1
func hashToPoint(suite abstract.Suite, msg []byte) abstract.Point {
	cipher := suite.Cipher(msg)
	p, _ := suite.Point().Pick(nil, cipher)
	return p
}

// H2
func hashToScalar(suite abstract.Suite, msg []byte) abstract.Scalar {
	cipher := suite.Cipher(msg)
	return suite.Scalar().Pick(cipher)
}
