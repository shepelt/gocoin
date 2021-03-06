package btc

import (
	"fmt"
	"bytes"
	"errors"
	"crypto/sha1"
	"encoding/hex"
	"crypto/sha256"
	"encoding/binary"
	"code.google.com/p/go.crypto/ripemd160"
)

const (
	MAX_SCRIPT_ELEMENT_SIZE = 520
)

func VerifyTxScript(sigScr []byte, pkScr []byte, i int, tx *Tx, p2sh bool) bool {
	if don(DBG_SCRIPT) {
		fmt.Println("VerifyTxScript", tx.Hash.String(), i+1, "/", len(tx.TxIn))
		fmt.Println("sigScript:", hex.EncodeToString(sigScr[:]))
		fmt.Println("_pkScript:", hex.EncodeToString(pkScr))
	}

	var st, stP2SH scrStack
	if !evalScript(sigScr, &st, tx, i) {
		if don(DBG_SCRERR) {
			fmt.Println("VerifyTxScript", tx.Hash.String(), i+1, "/", len(tx.TxIn))
			fmt.Println("sigScript failed :", hex.EncodeToString(sigScr[:]))
			fmt.Println("pkScript:", hex.EncodeToString(pkScr[:]))
		}
		return false
	}
	if don(DBG_SCRIPT) {
		fmt.Println("\nsigScr verified OK")
		//st.print()
		fmt.Println()
	}

	// copy the stack content to stP2SH
	if st.size()>0 {
		idx := -st.size()
		for i:=0; i<st.size(); i++ {
			x := st.top(idx)
			stP2SH.push(x)
			idx++
		}
	}

	if !evalScript(pkScr, &st, tx, i) {
		if don(DBG_SCRIPT) {
			fmt.Println("* pkScript failed :", hex.EncodeToString(pkScr[:]))
			fmt.Println("* VerifyTxScript", tx.Hash.String(), i+1, "/", len(tx.TxIn))
			fmt.Println("* sigScript:", hex.EncodeToString(sigScr[:]))
		}
		return false
	}

	if st.size()==0 {
		if don(DBG_SCRIPT) {
			fmt.Println("* stack empty after executing scripts:", hex.EncodeToString(pkScr[:]))
		}
		return false
	}

	if !st.popBool() {
		if don(DBG_SCRIPT) {
			fmt.Println("* FALSE on stack after executing scripts:", hex.EncodeToString(pkScr[:]))
		}
		return false
	}

	// Additional validation for spend-to-script-hash transactions:
	if p2sh && IsPayToScript(pkScr) {
		if don(DBG_SCRIPT) {
			fmt.Println()
			fmt.Println()
			fmt.Println(" ******************* Looks like P2SH script ********************* ")
			stP2SH.print()
		}

		if don(DBG_SCRERR) {
			fmt.Println("sigScr len", len(sigScr), hex.EncodeToString(sigScr))
		}
		if !IsPushOnly(sigScr) {
			if don(DBG_SCRERR) {
				fmt.Println("P2SH is not push only")
			}
			return false
		}

		pubKey2 := stP2SH.pop()
		if don(DBG_SCRIPT) {
			fmt.Println("pubKey2:", hex.EncodeToString(pubKey2))
		}

		if !evalScript(pubKey2, &stP2SH, tx, i) {
			if don(DBG_SCRERR) {
				println("P2SH extra verification failed")
			}
			return false
		}

		if stP2SH.size()==0 {
			if don(DBG_SCRIPT) {
				fmt.Println("* P2SH stack empty after executing script:", hex.EncodeToString(pubKey2))
			}
			return false
		}

		if !stP2SH.popBool() {
			if don(DBG_SCRIPT) {
				fmt.Println("* FALSE on stack after executing P2SH script:", hex.EncodeToString(pubKey2))
			}
			return false
		}
	}

	return true
}

func b2i(b bool) int64 {
	if b {
		return 1
	} else {
		return 0
	}
}

func evalScript(p []byte, stack *scrStack, tx *Tx, inp int) bool {
	if don(DBG_SCRIPT) {
		println("script len", len(p))
	}


	if len(p) > 10000 {
		if don(DBG_SCRERR) {
			println("script too long", len(p))
		}
		return false
	}

	defer func() {
		if r := recover(); r != nil {
			if don(DBG_SCRERR) {
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("pkg: %v", r)
				}
				println("evalScript panic:", err.Error())
			}
		}
	}()

	var vfExec scrStack
	var altstack scrStack
	sta, idx, opcnt := 0, 0, 0
	for idx < len(p) {
		fExec := vfExec.nofalse()

		// Read instruction
		opcode, vchPushValue, n, e := GetOpcode(p[idx:])
		if e!=nil {
			println(e.Error())
			println("A", idx, hex.EncodeToString(p))
			return false
		}
		idx+= n

		if don(DBG_SCRIPT) {
			fmt.Printf("\nExecuting opcode 0x%02x  n=%d  fExec:%t  push:%s..\n",
				opcode, n, fExec, hex.EncodeToString(vchPushValue))
			stack.print()
		}

		if vchPushValue!=nil && len(vchPushValue) > MAX_SCRIPT_ELEMENT_SIZE {
			if don(DBG_SCRERR) {
				println("vchPushValue too long", len(vchPushValue))
			}
			return false
		}

		if opcode > 0x60 {
			opcnt++
			if opcnt > 201 {
				if don(DBG_SCRERR) {
					println("evalScript: too many opcodes A")
				}
				return false
			}
		}

		if opcode == 0x7e/*OP_CAT*/ ||
			opcode == 0x7f/*OP_SUBSTR*/ ||
			opcode == 0x80/*OP_LEFT*/ ||
			opcode == 0x81/*OP_RIGHT*/ ||
			opcode == 0x83/*OP_INVERT*/ ||
			opcode == 0x84/*OP_AND*/ ||
			opcode == 0x85/*OP_OR*/ ||
			opcode == 0x86/*OP_XOR*/ ||
			opcode == 0x8d/*OP_2MUL*/ ||
			opcode == 0x8e/*OP_2DIV*/ ||
			opcode == 0x95/*OP_MUL*/ ||
			opcode == 0x96/*OP_DIV*/ ||
			opcode == 0x97/*OP_MOD*/ ||
			opcode == 0x98/*OP_LSHIFT*/ ||
			opcode == 0x99/*OP_RSHIFT*/ {
			if don(DBG_SCRERR) {
				println("Unsupported opcode", opcode)
			}
			return false
		}

		if fExec && 0<=opcode && opcode<=OP_PUSHDATA4 {
			stack.push(vchPushValue)
			if don(DBG_SCRIPT) {
				fmt.Println("pushed", len(vchPushValue), "bytes")
			}
		} else if fExec || (0x63/*OP_IF*/ <= opcode && opcode <= 0x68/*OP_ENDIF*/) {
			switch {
				case opcode==0x4f: // OP_1NEGATE
					stack.pushInt(-1)

				case opcode>=0x51 && opcode<=0x60: // OP_1-OP_16
					stack.pushInt(int64(opcode-0x50))

				case opcode==0x61: // OP_NOP
					// Do nothing

				/* - not handled
					OP_VER = 0x62
				*/

				case opcode==0x63 || opcode==0x64: //OP_IF || OP_NOTIF
					// <expression> if [statements] [else [statements]] endif
					fValue := false
					if fExec {
						if (stack.size() < 1) {
							if don(DBG_SCRERR) {
								println("Stack too short for", opcode)
							}
							return false
						}
						if opcode == 0x63/*OP_IF*/ {
							fValue = stack.popBool()
						} else {
							fValue = !stack.popBool()
						}
					}
					if don(DBG_SCRERR) {
						println(fExec, "if pushing", fValue, "...")
					}
					vfExec.pushBool(fValue)

				/* - not handled
				    OP_VERIF = 0x65,
				    OP_VERNOTIF = 0x66,
				*/
				case opcode==0x67: //OP_ELSE
					if vfExec.size()==0 {
						if don(DBG_SCRERR) {
							println("vfExec empty in OP_ELSE")
						}
					}
					vfExec.pushBool(!vfExec.popBool())

				case opcode==0x68: //OP_ENDIF
					if vfExec.size()==0 {
						if don(DBG_SCRERR) {
							println("vfExec empty in OP_ENDIF")
						}
					}
					vfExec.pop()

				case opcode==0x69: //OP_VERIFY
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					if !stack.topBool(-1) {
						return false
					}
					stack.pop()

				case opcode==0x6b: //OP_TOALTSTACK
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					altstack.push(stack.pop())

				case opcode==0x6c: //OP_FROMALTSTACK
					if altstack.size()<1 {
						if don(DBG_SCRERR) {
							println("AltStack too short for opcode", opcode)
						}
						return false
					}
					stack.push(altstack.pop())

				case opcode==0x6d: //OP_2DROP
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					stack.pop()
					stack.pop()

				case opcode==0x6e: //OP_2DUP
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					x1 := stack.top(-1)
					x2 := stack.top(-2)
					stack.push(x2)
					stack.push(x1)

				case opcode==0x6f: //OP_3DUP
					if stack.size()<3 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					x1 := stack.top(-3)
					x2 := stack.top(-2)
					x3 := stack.top(-1)
					stack.push(x1)
					stack.push(x2)
					stack.push(x3)

				case opcode==0x70: //OP_2OVER
					if stack.size()<4 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					x1 := stack.top(-4)
					x2 := stack.top(-3)
					stack.push(x1)
					stack.push(x2)

				case opcode==0x71: //OP_2ROT
					// (x1 x2 x3 x4 x5 x6 -- x3 x4 x5 x6 x1 x2)
					if stack.size()<6 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					x6 := stack.pop()
					x5 := stack.pop()
					x4 := stack.pop()
					x3 := stack.pop()
					x2 := stack.pop()
					x1 := stack.pop()
					stack.push(x3)
					stack.push(x4)
					stack.push(x5)
					stack.push(x6)
					stack.push(x1)
					stack.push(x2)

				case opcode==0x72: //OP_2SWAP
					// (x1 x2 x3 x4 -- x3 x4 x1 x2)
					if stack.size()<4 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					x4 := stack.pop()
					x3 := stack.pop()
					x2 := stack.pop()
					x1 := stack.pop()
					stack.push(x3)
					stack.push(x4)
					stack.push(x1)
					stack.push(x2)

				case opcode==0x73: //OP_IFDUP
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					if stack.topBool(-1) {
						stack.push(stack.top(-1))
					}

				case opcode==0x74: //OP_DEPTH
					stack.pushInt(int64(stack.size()))

				case opcode==0x75: //OP_DROP
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					stack.pop()

				case opcode==0x76: //OP_DUP
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					el := stack.pop()
					stack.push(el)
					stack.push(el)

				case opcode==0x77: //OP_NIP
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					x := stack.pop()
					stack.pop()
					stack.push(x)

				case opcode==0x78: //OP_OVER
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					stack.push(stack.top(-2))

				case opcode==0x79 || opcode==0x7a: //OP_PICK || OP_ROLL
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					n := stack.popInt()
					if n < 0 || n >= int64(stack.size()) {
						if don(DBG_SCRERR) {
							println("Wrong n for opcode", opcode)
						}
						return false
					}
					if opcode==0x79/*OP_PICK*/ {
						stack.push(stack.top(int(-1-n)))
					} else if n > 0 {
						tmp := make([][]byte, n)
						for i := range tmp {
							tmp[i] = stack.pop()
						}
						xn := stack.pop()
						for i := len(tmp)-1; i>=0; i-- {
							stack.push(tmp[i])
						}
						stack.push(xn)
					}

				case opcode==0x7b: //OP_ROT
					if stack.size()<3 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					x3 := stack.pop()
					x2 := stack.pop()
					x1 := stack.pop()
					stack.push(x2)
					stack.push(x3)
					stack.push(x1)

				case opcode==0x7c: //OP_SWAP
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					x1 := stack.pop()
					x2 := stack.pop()
					stack.push(x1)
					stack.push(x2)

				case opcode==0x7d: //OP_TUCK
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					x1 := stack.pop()
					x2 := stack.pop()
					stack.push(x1)
					stack.push(x2)
					stack.push(x1)

				case opcode==0x82: //OP_SIZE
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					stack.pushInt(int64(len(stack.top(-1))))

				case opcode==0x87 || opcode==0x88: //OP_EQUAL || OP_EQUALVERIFY
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					a := stack.pop()
					b := stack.pop()
					if opcode==0x88 { //OP_EQUALVERIFY
						if !bytes.Equal(a, b) {
							return false
						}
					} else {
						stack.pushBool(bytes.Equal(a, b))
					}

				/* - not handled
					OP_RESERVED1 = 0x89,
					OP_RESERVED2 = 0x8a,
				*/

				case opcode==0x8b: //OP_1ADD
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					stack.pushInt(stack.popInt()+1)

				case opcode==0x8c: //OP_1SUB
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					stack.pushInt(stack.popInt()-1)

				case opcode==0x8f: //OP_NEGATE
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					stack.pushInt(-stack.popInt())

				case opcode==0x90: //OP_ABS
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					a := stack.popInt()
					if a<0 {
						stack.pushInt(-a)
					} else {
						stack.pushInt(a)
					}

				case opcode==0x91: //OP_NOT
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					stack.pushBool(stack.popInt()==0)

				case opcode==0x92: //OP_0NOTEQUAL
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					stack.pushBool(stack.popBool())

				case opcode==0x93 || //OP_ADD
					opcode==0x94 || //OP_SUB
					opcode==0x9a || //OP_BOOLAND
					opcode==0x9b || //OP_BOOLOR
					opcode==0x9c || opcode==0x9d || //OP_NUMEQUAL || OP_NUMEQUALVERIFY
					opcode==0x9e || //OP_NUMNOTEQUAL
					opcode==0x9f || //OP_LESSTHAN
					opcode==0xa0 || //OP_GREATERTHAN
					opcode==0xa1 || //OP_LESSTHANOREQUAL
					opcode==0xa2 || //OP_GREATERTHANOREQUAL
					opcode==0xa3 || //OP_MIN
					opcode==0xa4: //OP_MAX
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					bn2 := stack.popInt()
					bn1 := stack.popInt()
					var bn int64
					switch opcode {
						case 0x93: bn = bn1 + bn2 // OP_ADD
						case 0x94: bn = bn1 - bn2 // OP_SUB
						case 0x9a: bn = b2i(bn1 != 0 && bn2 != 0) // OP_BOOLAND
						case 0x9b: bn = b2i(bn1 != 0 || bn2 != 0) // OP_BOOLOR
						case 0x9c: bn = b2i(bn1 == bn2) // OP_NUMEQUAL
						case 0x9d: bn = b2i(bn1 == bn2) // OP_NUMEQUALVERIFY
						case 0x9e: bn = b2i(bn1 != bn2) // OP_NUMNOTEQUAL
						case 0x9f: bn = b2i(bn1 < bn2) // OP_LESSTHAN
						case 0xa0: bn = b2i(bn1 > bn2) // OP_GREATERTHAN
						case 0xa1: bn = b2i(bn1 <= bn2) // OP_LESSTHANOREQUAL
						case 0xa2: bn = b2i(bn1 >= bn2) // OP_GREATERTHANOREQUAL
						case 0xa3: // OP_MIN
							if bn1 < bn2 {
								bn = bn1
							} else {
								bn = bn2
							}
						case 0xa4: // OP_MAX
							if bn1 > bn2 {
								bn = bn1
							} else {
								bn = bn2
							}
						default: panic("invalid opcode")
					}
					if opcode == 0x9d { //OP_NUMEQUALVERIFY
						if bn==0 {
							return false
						}
					} else {
						stack.pushInt(bn)
					}

				case opcode==0xa5: //OP_WITHIN
					if stack.size()<3 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					bn3 := stack.popInt()
					bn2 := stack.popInt()
					bn1 := stack.popInt()
					stack.pushBool(bn2 <= bn1 && bn1 < bn3)

				case opcode==0xa6: //OP_RIPEMD160
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					rim := ripemd160.New()
					rim.Write(stack.pop()[:])
					stack.push(rim.Sum(nil)[:])

				case opcode==0xa7: //OP_SHA1
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					sha := sha1.New()
					sha.Write(stack.pop()[:])
					stack.push(sha.Sum(nil)[:])

				case opcode==0xa8: //OP_SHA256
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					sha := sha256.New()
					sha.Write(stack.pop()[:])
					stack.push(sha.Sum(nil)[:])

				case opcode==0xa9: //OP_HASH160
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					rim160 := Rimp160AfterSha256(stack.pop())
					stack.push(rim160[:])

				case opcode==0xaa: //OP_HASH256
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					h := Sha2Sum(stack.pop())
					stack.push(h[:])

				case opcode==0xab: // OP_CODESEPARATOR
					sta = idx

				case opcode==0xac || opcode==0xad: // OP_CHECKSIG || OP_CHECKSIGVERIFY
					if stack.size()<2 {
						if don(DBG_SCRERR) {
							println("Stack too short for opcode", opcode)
						}
						return false
					}
					var ok bool
					pk := stack.pop()
					si := stack.pop()
					if len(si) > 9 {
						sh := tx.SignatureHash(delSig(p[sta:], si), inp, si[len(si)-1])
						ok = EcdsaVerify(pk, si, sh)
						if !ok && don(DBG_SCRERR) {
							println("EcdsaVerify fail 1")
						}
					}
					if don(DBG_SCRIPT) {
						println("ver:", ok)
					}
					if opcode==0xad {
						if !ok { // OP_CHECKSIGVERIFY
							return false
						}
					} else { // OP_CHECKSIG
						stack.pushBool(ok)
					}

				case opcode==0xae || opcode==0xaf: //OP_CHECKMULTISIG || OP_CHECKMULTISIGVERIFY
					//println("OP_CHECKMULTISIG ...")
					//stack.print()
					if stack.size()<1 {
						if don(DBG_SCRERR) {
							println("OP_CHECKMULTISIG: Stack too short A")
						}
						return false
					}
					i := 1
					keyscnt := stack.topInt(-i)
					if keyscnt < 0 || keyscnt > 20 {
						println("OP_CHECKMULTISIG: Wrong number of keys")
						return false
					}
					opcnt += int(keyscnt)
					if opcnt > 201 {
						println("evalScript: too many opcodes B")
						return false
					}
					i++
					ikey := i
					i += int(keyscnt)
					if stack.size()<i {
						if don(DBG_SCRERR) {
							println("OP_CHECKMULTISIG: Stack too short B")
						}
						return false
					}
					sigscnt := stack.topInt(-i)
					if sigscnt < 0 || sigscnt > keyscnt {
						println("OP_CHECKMULTISIG: sigscnt error")
						return false
					}
					i++
					isig := i
					i += int(sigscnt)
					if stack.size()<i {
						if don(DBG_SCRERR) {
							println("OP_CHECKMULTISIG: Stack too short C")
						}
						return false
					}

					xxx := p[sta:]
					for k:=0; k<int(sigscnt); k++ {
						xxx = delSig(xxx, stack.top(-isig-k))
					}

					success := true
					for sigscnt > 0 {
						pk := stack.top(-ikey)
						si := stack.top(-isig)
						if len(si)>9 && ((len(pk)==65 && pk[0]==4) || (len(pk)==33 && (pk[0]|1)==3)) {
							sh := tx.SignatureHash(xxx, inp, si[len(si)-1])
							if EcdsaVerify(pk, si, sh) {
								isig++
								sigscnt--
							}
						}

						ikey++
						keyscnt--

						// If there are more signatures left than keys left,
						// then too many signatures have failed
						if sigscnt > keyscnt {
							success = false
							break
						}
					}
					for i > 0 {
						i--
						stack.pop()
					}
					if opcode==0xaf {
						if !success { // OP_CHECKMULTISIGVERIFY
							return false
						}
					} else {
						stack.pushBool(success)
					}

				case opcode>=0xb0 && opcode<=0xb9: //OP_NOP
					// just do nothing

				default:
					if don(DBG_SCRERR) {
						fmt.Printf("Unhandled opcode 0x%02x - a handler must be implemented\n", opcode)
						stack.print()
						fmt.Println("Rest of the script:", hex.EncodeToString(p[idx:]))
					}
					return false
			}
		}

		if don(DBG_SCRIPT) {
			fmt.Printf("Finished Executing opcode 0x%02x\n", opcode)
			stack.print()
		}
		if (stack.size() + altstack.size() > 1000) {
			if don(DBG_SCRERR) {
				println("Stack too big")
			}
			return false
		}
	}

	if don(DBG_SCRIPT) {
		fmt.Println("END OF SCRIPT")
		stack.print()
	}

	if vfExec.size()>0 {
		if don(DBG_SCRERR) {
			println("Unfinished if..")
		}
		return false
	}

	return true
}


func delSig(where, sig []byte) (res []byte) {
	// recover the standard length
	bb := new(bytes.Buffer)
	if len(sig) < OP_PUSHDATA1 {
		bb.Write([]byte{byte(len(sig))})
	} else if len(sig) <= 0xff {
		bb.Write([]byte{OP_PUSHDATA1})
		bb.Write([]byte{byte(len(sig))})
	} else if len(sig) <= 0xffff {
		bb.Write([]byte{OP_PUSHDATA2})
		binary.Write(bb, binary.LittleEndian, uint16(len(sig)))
	} else {
		bb.Write([]byte{OP_PUSHDATA4})
		binary.Write(bb, binary.LittleEndian, uint32(len(sig)))
	}
	bb.Write(sig)
	sig = bb.Bytes()
	var idx int
	for idx < len(where) {
		_, _, n, e := GetOpcode(where[idx:])
		if e!=nil {
			println(e.Error())
			println("B", idx, hex.EncodeToString(where))
			return
		}
		if !bytes.Equal(where[idx:idx+n], sig) {
			res = append(res, where[idx:idx+n]...)
		}
		idx+= n
	}
	return
}


func GetOpcode(b []byte) (opcode int, pvchRet []byte, pc int, e error) {
	// Read instruction
	if pc+1 > len(b) {
		e = errors.New("GetOpcode error 1")
		return
	}
	opcode = int(b[pc])
	pc++

	if opcode <= OP_PUSHDATA4 {
		nSize := 0
		if opcode < OP_PUSHDATA1 {
			nSize = opcode
		}
		if opcode == OP_PUSHDATA1 {
			if pc+1 > len(b) {
				e = errors.New("GetOpcode error 2")
				return
			}
			nSize = int(b[pc])
			pc++
		} else if opcode == OP_PUSHDATA2 {
			if pc+2 > len(b) {
				e = errors.New("GetOpcode error 3")
				return
			}
			nSize = int(binary.LittleEndian.Uint16(b[pc:pc+2]))
			pc += 2
		} else if opcode == OP_PUSHDATA4 {
			if pc+4 > len(b) {
				e = errors.New("GetOpcode error 4")
				return
			}
			nSize = int(binary.LittleEndian.Uint16(b[pc:pc+4]))
			pc += 4
		}
		if pc+nSize > len(b) {
			e = errors.New(fmt.Sprint("GetOpcode size to fetch exceeds remainig data left: ", pc+nSize, "/", len(b)))
			return
		}
		pvchRet = b[pc:pc+nSize]
		pc += nSize
	}

	return
}
