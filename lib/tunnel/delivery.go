package tunnel

import (
	"encoding/binary"
	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/hkparker/go-i2p/lib/common"
)

/*
I2P First Fragment Delivery Instructions
https://geti2p.net/spec/tunnel-message#struct-tunnelmessagedeliveryinstructions
Accurate for version 0.9.11

+----+----+----+----+----+----+----+----+
|flag|  Tunnel ID (opt)  |              |
+----+----+----+----+----+              +
|                                       |
+                                       +
|         To Hash (optional)            |
+                                       +
|                                       |
+                        +--------------+
|                        |dly | Message
+----+----+----+----+----+----+----+----+
 ID (opt) |extended opts (opt)|  size   |
+----+----+----+----+----+----+----+----+

flag ::
       1 byte
       Bit order: 76543210
       bit 7: 0 to specify an initial fragment or an unfragmented message
       bits 6-5: delivery type
                 0x0 = LOCAL
                 0x01 = TUNNEL
                 0x02 = ROUTER
                 0x03 = unused, invalid
                 Note: LOCAL is used for inbound tunnels only, unimplemented
                 for outbound tunnels
       bit 4: delay included?  Unimplemented, always 0
                               If 1, a delay byte is included
       bit 3: fragmented?  If 0, the message is not fragmented, what follows
                           is the entire message
                           If 1, the message is fragmented, and the
                           instructions contain a Message ID
       bit 2: extended options?  Unimplemented, always 0
                                 If 1, extended options are included
       bits 1-0: reserved, set to 0 for compatibility with future uses

Tunnel ID :: TunnelId
       4 bytes
       Optional, present if delivery type is TUNNEL
       The destination tunnel ID

To Hash ::
       32 bytes
       Optional, present if delivery type is ROUTER, or TUNNEL			See: https://trac.i2p2.de/ticket/1845#ticket
          If ROUTER, the SHA256 Hash of the router
          If TUNNEL, the SHA256 Hash of the gateway router

Delay ::
       1 byte
       Optional, present if delay included flag is set
       In tunnel messages: Unimplemented, never present; original
       specification:
          bit 7: type (0 = strict, 1 = randomized)
          bits 6-0: delay exponent (2^value minutes)

Message ID ::
       4 bytes
       Optional, present if this message is the first of 2 or more fragments
          (i.e. if the fragmented bit is 1)
       An ID that uniquely identifies all fragments as belonging to a single
       message (the current implementation uses I2NPMessageHeader.msg_id)

Extended Options ::
       2 or more bytes
       Optional, present if extend options flag is set
       Unimplemented, never present; original specification:
       One byte length and then that many bytes

size ::
       2 bytes
       The length of the fragment that follows
       Valid values: 1 to approx. 960 in a tunnel message

Total length: Typical length is:
       3 bytes for LOCAL delivery (tunnel message);
       35 bytes for ROUTER / DESTINATION delivery or 39 bytes for TUNNEL
       delivery (unfragmented tunnel message);
       39 bytes for ROUTER delivery or 43 bytes for TUNNEL delivery (first
       fragment)



I2P Follow-on Fragment Delivery Instructions
https://geti2p.net/spec/tunnel-message#struct-tunnelmessagedeliveryinstructions
Accurate for version 0.9.11

----+----+----+----+----+----+----+
|frag|     Message ID    |  size   |
+----+----+----+----+----+----+----+

frag ::
       1 byte
       Bit order: 76543210
       binary 1nnnnnnd
              bit 7: 1 to indicate this is a follow-on fragment
              bits 6-1: nnnnnn is the 6 bit fragment number from 1 to 63
              bit 0: d is 1 to indicate the last fragment, 0 otherwise

Message ID ::
       4 bytes
       Identifies the fragment sequence that this fragment belongs to.
       This will match the message ID of an initial fragment (a fragment
       with flag bit 7 set to 0 and flag bit 3 set to 1).

size ::
       2 bytes
       the length of the fragment that follows
       valid values: 1 to 996

total length: 7 bytes
*/

const (
	DT_LOCAL = iota
	DT_TUNNEL
	DT_ROUTER
	DT_UNUSED
)

const (
	FIRST_FRAGMENT = iota
	FOLLOW_ON_FRAGMENT
)

type DelayFactor byte

type DeliveryInstructions []byte

// Return if the DeliveryInstructions are of type FIRST_FRAGMENT or FOLLOW_ON_FRAGMENT.
func (delivery_instructions DeliveryInstructions) Type() (int, error) {
	if len(delivery_instructions) >= 1 {
		/*
			 Check if the 7 bit of the Delivery Instructions
			 is set using binary AND operator to determine
			 the Delivery Instructions type

			      1xxxxxxx	      0xxxxxxx
			     &10000000	     &10000000
			     ---------	     ---------
			      10000000	      00000000

			  bit is set,		bit is not set,
			  message is a		message is an
			  follow-on fragment	initial I2NP message
						fragment or a complete fragment
		*/
		if (delivery_instructions[0] & 0x08) == 0x08 {
			return FOLLOW_ON_FRAGMENT, nil
		}
		return FIRST_FRAGMENT, nil
	}
	return 0, errors.New("DeliveryInstructions contains no data")
}

// Return the delivery type for these DeliveryInstructions, can be of type
// DT_LOCAL, DT_TUNNEL, DT_ROUTER, or DT_UNUSED.
func (delivery_instructions DeliveryInstructions) DeliveryType() (byte, error) {
	if len(delivery_instructions) >= 1 {
		/*
		 Check if the 6-5 bits of the Delivery Instructions
		 are set using binary AND operator to determine
		 the delivery type

		      xx0?xxxx
		     &00110000    bit shift
		     ---------
		      000?0000       >> 4   =>   n	(DT_* consts)
		*/
		return ((delivery_instructions[0] & 0x30) >> 4), nil
	}
	return 0, errors.New("DeliveryInstructions contains no data")
}

// Check if the delay bit is set.  This feature in unimplemented in the Java router.
func (delivery_instructions DeliveryInstructions) HasDelay() (bool, error) {
	if len(delivery_instructions) >= 1 {
		/*
			 Check if the 4 bit of the Delivery Instructions
			 is set using binary AND operator to determine
			 if the Delivery Instructions has a delay

			      xxx1xxxx	      xxx0xxxx
			     &00010000	     &00010000
			     ---------	     ---------
			      00010000	      00000000

			  bit is set,		bit is not set,
			  delay is included     no delay included

			Delay is unimplemented in the Java router, a warning
			is logged as this is interesting behavior.
		*/
		delay := (delivery_instructions[0] & 0x10) == 0x10
		if delay {
			log.WithFields(log.Fields{
				"at":   "(DeliveryInstructions) HasDelay",
				"info": "this feature is unimplemented in the Java router",
			}).Warn("DeliveryInstructions found with delay bit set")
		}
		return delay, nil
	}
	return false, errors.New("DeliveryInstructions contains no data")
}

// Returns true if the Delivery Instructions are fragmented or false
// if the following data contains the entire message
func (delivery_instructions DeliveryInstructions) Fragmented() (bool, error) {
	if len(delivery_instructions) >= 1 {
		/*
		 Check if the 3 bit of the Delivery Instructions
		 is set using binary AND operator to determine
		 if the Delivery Instructions is fragmented or if
		 the entire message is contained in the following data

		      xxxx1xxx	      xxxx0xxx
		     &00001000	     &00001000
		     ---------	     ---------
		      00001000	      00000000

		  bit is set,		bit is not set,
		  message is		message is not
		  fragmented		fragmented
		*/
		return ((delivery_instructions[0] & 0x08) == 0x08), nil
	}
	return false, errors.New("DeliveryInstructions contains no data")
}

// Check if the extended options bit is set.  This feature in unimplemented in the Java router.
func (delivery_instructions DeliveryInstructions) HasExtendedOptions() (bool, error) {
	if len(delivery_instructions) >= 1 {
		/*
			 Check if the 2 bit of the Delivery Instructions
			 is set using binary AND operator to determine
			 if the Delivery Instructions has a extended options

			      xxxxx1xx	      xxxxx0xx
			     &00000100	     &00000100
			     ---------	     ---------
			      00000100	      00000000

			  bit is set,		bit is not set,
			  extended options      extended options
			  included		not included

			Extended options is unimplemented in the Java router, a warning
			is logged as this is interesting behavior.
		*/
		extended_options := (delivery_instructions[0] & 0x04) == 0x04
		if extended_options {
			log.WithFields(log.Fields{
				"at":   "(DeliveryInstructions) ExtendedOptions",
				"info": "this feature is unimplemented in the Java router",
			}).Warn("DeliveryInstructions found with extended_options bit set")
		}
		return extended_options, nil
	}
	return false, errors.New("DeliveryInstructions contains no data")
}

// Return the tunnel ID in this DeliveryInstructions or 0 and an error if the
// DeliveryInstructions are not of type DT_TUNNEL.
func (delivery_instructions DeliveryInstructions) TunnelID() (tunnel_id uint32, err error) {
	di_type, err := delivery_instructions.DeliveryType()
	if err != nil {
		return
	}
	if di_type == DT_TUNNEL {
		if len(delivery_instructions) >= 5 {
			tunnel_id = binary.BigEndian.Uint32(delivery_instructions[1:5])
		} else {
			err = errors.New("DeliveryInstructions are invalid, too little data for Tunnel ID")
		}
	} else {
		err = errors.New("DeliveryInstructions are not of type DT_TUNNEL")
	}
	return
}

// Return the hash for these DeliveryInstructions, which varies by hash type.
//  If the type is DT_TUNNEL, hash is the SHA256 of the gateway router, if
//  the type is DT_ROUTER it is the SHA256 of the router.
func (delivery_instructions DeliveryInstructions) Hash() (hash common.Hash, err error) {
	delivery_type, err := delivery_instructions.DeliveryType()
	if err != nil {
		return
	}
	hash_start := 1
	hash_end := 33
	if delivery_type == DT_TUNNEL {
		// add 4 bytes for DT_TUNNEL's TunnelID
		hash_start := hash_start + 4
		hash_end := hash_end + 4
		if len(delivery_instructions) >= hash_end {
			copy(hash[:], delivery_instructions[hash_start:hash_end])
		} else {
			err = errors.New("DeliveryInstructions is invalid, not contain enough data for hash given type DT_TUNNEL")
		}
	} else if delivery_type == DT_ROUTER {
		if len(delivery_instructions) >= hash_end {
			copy(hash[:], delivery_instructions[hash_start:hash_end])
		} else {
			err = errors.New("DeliveryInstructions is invalid, not contain enough data for hash given type DT_ROUTER")
		}
	} else {
		err = errors.New("No Hash on DeliveryInstructions not of type DT_TUNNEL or DT_ROUTER")
	}
	return
}

// Return the DelayFactor if present and any errors encountered parsing the DeliveryInstructions.
func (delivery_instructions DeliveryInstructions) Delay() (delay_factor DelayFactor, err error) {
	delay, err := delivery_instructions.HasDelay()
	if err != nil {
		return
	}
	if delay {
		var di_type byte
		di_type, err = delivery_instructions.DeliveryType()
		if err != nil {
			return
		}
		if di_type == DT_TUNNEL {
			if len(delivery_instructions) >= 37 {
				delay_factor = DelayFactor(delivery_instructions[37])
			} else {
				err = errors.New("DeliveryInstructions is invalid, does not contain enough data for DelayFactor")
				return
			}
		} else if di_type == DT_ROUTER {
			if len(delivery_instructions) >= 36 {
				delay_factor = DelayFactor(delivery_instructions[36])
			} else {
				err = errors.New("DeliveryInstructions is invalid, does not contain enough data for DelayFactor")
				return
			}
		} else {
			log.WithFields(log.Fields{
				"at": "(DeliveryInstructions) Delay",
			}).Warn("Delay not present on DeliveryInstructions not of type DT_TUNNEL or DT_ROUTER")
		}
	}
	return
}

// Return the I2NP Message ID or 0 and an error if the data is not available for this
// DeliveryInstructions.
func (delivery_instructions DeliveryInstructions) MessageID() (msgid uint32, err error) {
	di_type, err := delivery_instructions.Type()
	if err != nil {
		return
	}
	if di_type == FOLLOW_ON_FRAGMENT {
		if len(delivery_instructions) >= 5 {
			msgid = binary.BigEndian.Uint32(delivery_instructions[1:5])
		} else {
			err = errors.New("DeliveryInstructions are invalid, not enough data for Message ID")
		}
	} else if di_type == FIRST_FRAGMENT {
		var message_id_index int
		message_id_index, err = delivery_instructions.message_id_index()
		if err != nil {
			return
		}
		if len(delivery_instructions) >= message_id_index+4 {
			msgid = binary.BigEndian.Uint32(delivery_instructions[message_id_index : message_id_index+4])
		} else {
			err = errors.New("DeliveryInstructions are invalid, not enough data for Message ID")
		}
	} else {
		err = errors.New("No Message ID for DeliveryInstructions not of type FIRST_FRAGMENT or FOLLOW_ON_FRAGMENT")
	}
	return
}

// Return the Extended Options data if present, or an error if not present.  Extended Options in unimplemented
// in the Java router and the presence of extended options will generate a warning.
func (delivery_instructions DeliveryInstructions) ExtendedOptions() (data []byte, err error) {
	ops, err := delivery_instructions.HasExtendedOptions()
	if err != nil {
		return
	}
	if ops {
		var extended_options_index int
		extended_options_index, err = delivery_instructions.extended_options_index()
		if err != nil {
			return
		}
		if len(delivery_instructions) < extended_options_index+2 {
			err = errors.New("DeliveryInstructions are invalid, length is shorter than required for Extended Options")
			return
		} else {
			extended_options_size := common.Integer([]byte{delivery_instructions[extended_options_index]})
			if len(delivery_instructions) < extended_options_index+1+extended_options_size {
				err = errors.New("DeliveryInstructions are invalid, length is shorter than specified in Extended Options")
				return
			} else {
				data = delivery_instructions[extended_options_index+1 : extended_options_size]
				return
			}

		}
	} else {
		err = errors.New("DeliveryInstruction does not have the ExtendedOptions flag set")
	}
	return
}

// Return the size of the associated I2NP fragment and an error if the data is unavailable.
func (delivery_instructions DeliveryInstructions) FragmentSize() (frag_size uint16, err error) {
	di_type, err := delivery_instructions.Type()
	if err != nil {
		return
	}
	if di_type == FOLLOW_ON_FRAGMENT {
		if len(delivery_instructions) >= 7 {
			frag_size = binary.BigEndian.Uint16(delivery_instructions[5:7])
		} else {
			err = errors.New("DeliveryInstructions are invalid, not enough data for Fragment Size")
		}
	} else if di_type == FIRST_FRAGMENT {
		var fragment_size_index int
		fragment_size_index, err = delivery_instructions.fragment_size_index()
		if err != nil {
			return
		}
		if len(delivery_instructions) >= fragment_size_index+2 {
			frag_size = binary.BigEndian.Uint16(delivery_instructions[fragment_size_index : fragment_size_index+2])
		} else {
			err = errors.New("DeliveryInstructions are invalid, not enough data for Fragment Size")
		}
	} else {
		err = errors.New("No Fragment Size for DeliveryInstructions not of type FIRST_FRAGMENT or FOLLOW_ON_FRAGMENT")
	}
	return
}

// Find the correct index for the Message ID in a FIRST_FRAGMENT DeliveryInstructions
func (delivery_instructions DeliveryInstructions) message_id_index() (message_id int, err error) {
	fragmented, err := delivery_instructions.Fragmented()
	if err != nil {
		return
	}
	if fragmented {
		// Start counting after the flags
		message_id = 1

		// Add the Tunnel ID and Hash if present
		var di_type byte
		di_type, err = delivery_instructions.DeliveryType()
		if err != nil {
			return
		}
		if di_type == DT_TUNNEL {
			message_id += 36
		} else if di_type == DT_ROUTER {
			message_id += 32
		}

		// Add the Delay if present
		var delay bool
		delay, err = delivery_instructions.HasDelay()
		if err != nil {
			return
		}
		if delay {
			message_id++
		}

		return message_id, nil
	} else {
		return 0, errors.New("DeliveryInstruction must be fragmented to have a Message ID")
	}
}

func (delivery_instructions DeliveryInstructions) extended_options_index() (extended_options int, err error) {
	return
}

func (delivery_instructions DeliveryInstructions) fragment_size_index() (fragment_size int, err error) {
	//fragment_size = 5
	//t := delivery_instructions.DeliveryType()
	//if t == DT_TUNNEL {
	//	idx += 36
	//} else if t == DT_ROUTER {
	//	idx += 32
	//}
	//if delivery_instructions.HasDelay() {
	//	idx++
	//}
	//if delivery_instructions.HasExtendedOptions() {
	//	// add extended options length to idx
	//}
	return 0, nil
}

func readDeliveryInstructions(data []byte) (instructions DeliveryInstructions, remainder []byte, err error) {
	return
}
